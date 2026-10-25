package hooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
)

// recordEventAndAllow inserts a tool_call agent_event row for observability
// and returns an allow result. Used by the plan mode bypass and the normal
// flow to avoid duplicating the event recording logic.
func recordEventAndAllow(event *CloudEvent, ctx *toolUseContext, database *sql.DB) (*HookResult, error) {
	inputSummary := SummariseInput(event.ToolName, event.ToolInput)

	// Normalize path fields to repo-relative before storage so that captured
	// artifacts remain stable across machines and worktrees. Bash "command"
	// fields are deliberately excluded (see normalizeToolInputPaths).
	toolInputStr := marshalToolInput(event.ToolName, event.ToolInput)

	ev := newToolCallEvent(event, ctx, inputSummary, toolInputStr)

	// Route the tool_call agent_events INSERT through the daemon-first enqueue
	// path (plan-2390966a slice-4). This is the hot write that stalled 5–14s
	// under a held lock; RouteInsertEvent opens NO direct writable handle when
	// the daemon is reachable and degrades to a <1s bounded fallback otherwise.
	// Best-effort/advisory like the prior db.InsertEvent — never blocks the hook.
	_ = RouteInsertEvent("pretooluse", ctx.ProjectDir, ctx.SessionID, ev, database)

	// Canonical mirror of the same tool call (bug-a3b17225). The row above only
	// ever lands in the real index; the read-only hook path that the research
	// and UI-validation guards run on opens an EMPTY projection and can never
	// read it back, which left those guards permanently fail-open. This short
	// append gives them a durable file source. Best-effort and bounded.
	appendCanonicalToolEvent(ctx.ProjectDir, ctx.SessionID, canonicalToolEvent{
		Tool:    event.ToolName,
		Agent:   ctx.AgentID,
		Summary: inputSummary,
		Input:   toolInputStr,
	})

	routeClaimBookkeeping(event, ctx)

	os.Setenv("WIPNOTE_CURRENT_EVENT_ID", ev.EventID)

	// Capture the parent UserQuery event ID at PreToolUse time (before the tool
	// executes) and persist it to CLAUDE_ENV_FILE so PostToolUse reads the same
	// parent even when a new UserQuery has been inserted while the tool ran.
	// This eliminates the race in resolveParentEventID's LatestEventByTool fallback.
	if ctx.ParentEventID != "" {
		writeParentPromptEvent(ctx.ParentEventID)
	}

	result := &HookResult{}
	// bug-190950e0: this is the single choke point every "allow" exit of
	// PreToolUse passes through, so patching it here (rather than each call
	// site) covers plan-mode bypass, the file-overlap-advisory path, and the
	// plain allow path uniformly, without touching any block/gate decision
	// above — this only ever runs once a tool call has already been allowed.
	applyClaimAgentPropagation(event, result)
	return result, nil
}

// newToolCallEvent builds the started tool_call agent_event for this call.
func newToolCallEvent(event *CloudEvent, ctx *toolUseContext, inputSummary, toolInputStr string) *models.AgentEvent {
	return &models.AgentEvent{
		// Deterministic per-call id (bug-07fd4a51) so PostToolUse upserts this
		// same row instead of searching for it by tool name.
		EventID:       toolCallEventIDOrRandom(ctx.SessionID, event.ToolUseID),
		AgentID:       ctx.AgentID,
		EventType:     models.EventToolCall,
		Timestamp:     time.Now().UTC(),
		ToolName:      event.ToolName,
		InputSummary:  inputSummary,
		ToolInput:     toolInputStr,
		SessionID:     ctx.SessionID,
		FeatureID:     ctx.FeatureID,
		ParentEventID: ctx.ParentEventID,
		SubagentType:  ctx.AgentType,
		Status:        "started",
		StepID:        event.ToolUseID,
		Source:        "hook",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

// routeClaimBookkeeping heartbeats the active claim and reaps expired claims.
func routeClaimBookkeeping(event *CloudEvent, ctx *toolUseContext) {
	// Claim bookkeeping (bug-d792aee6 finding 2): route BOTH claim writes through
	// the daemon-first enqueue-only seam (RouteHookWrite) instead of issuing them
	// directly on the 5s-busy_timeout hook handle. Under a held external write
	// lock the direct path stalled ~5s, defeating the <1s hot-hook bound; the
	// builders return the exact (sql, args) the originals Exec, JSON-transport-safe
	// so the daemon can re-bind them. Both stay best-effort: the advisory bool is
	// ignored, and canonical NDJSON + reindex are the durability backstop.
	if ctx.FeatureID != "" {
		writePath := paths.MustNormalize(extractFilePath(event.ToolInput), "")
		hbSQL, hbArgs := db.HeartbeatClaimByWorkItemStmt(ctx.FeatureID, ctx.SessionID, writePath, 30*time.Minute)
		_ = RouteHookWrite("pretooluse", ctx.ProjectDir, ctx.SessionID, hbSQL, hbArgs...)
	}
	reapSQL, reapArgs := db.ReapExpiredClaimsStmt()
	_ = RouteHookWrite("pretooluse", ctx.ProjectDir, ctx.SessionID, reapSQL, reapArgs...)
}

// writeParentPromptEvent persists parentEventID as WIPNOTE_PARENT_PROMPT_EVENT
// to CLAUDE_ENV_FILE so PostToolUse hook processes can read the correct parent
// without querying the DB (which would return the wrong UserQuery when a new
// prompt has arrived since the tool started).
//
// Falls back to os.Setenv only (no-op for PostToolUse) when CLAUDE_ENV_FILE is
// unset — the existing LatestEventByTool DB fallback remains correct in that case.
func writeParentPromptEvent(parentEventID string) {
	// Keep the in-process env var current so any same-process callers see it.
	os.Setenv("WIPNOTE_PARENT_PROMPT_EVENT", parentEventID)

	envFile := os.Getenv("CLAUDE_ENV_FILE")
	if envFile == "" {
		// CLAUDE_ENV_FILE unset (YOLO mode, worktree subagents, or plugin-dir
		// launches). The in-process os.Setenv above covers same-process callers;
		// PostToolUse will fall through to the existing DB fallback chain.
		return
	}
	f, err := os.OpenFile(envFile, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "export WIPNOTE_PARENT_PROMPT_EVENT=%s\n", parentEventID)
}

// SummariseInput builds a short human-readable summary of tool input.
func SummariseInput(toolName string, input map[string]any) string {
	if input == nil {
		return toolName
	}

	// Read tool: include offset/limit as line range suffix.
	if toolName == "Read" {
		return summariseReadInput(input)
	}

	// For file tools, use the path.
	for _, key := range []string{"path", "file_path", "command", "cmd", "query", "prompt"} {
		if v, ok := input[key].(string); ok && v != "" {
			if len(v) > 120 {
				v = v[:120] + "…"
			}
			return v
		}
	}
	// Fallback: compact JSON of first 200 chars.
	b, _ := json.Marshal(input)
	s := string(b)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// summariseReadInput builds a summary for the Read tool that includes the file
// path and optional line range from offset/limit parameters.
// Examples:
//
//	"/path/to/file.go"              — no offset/limit
//	"/path/to/file.go [100:150]"    — offset=100, limit=50
//	"/path/to/file.go [100:]"       — offset=100, no limit
//	"/path/to/file.go [:50]"        — no offset, limit=50
func summariseReadInput(input map[string]any) string {
	filePath := extractFilePath(input)
	if filePath == "" {
		return "Read"
	}

	offset := toInt(input["offset"])
	limit := toInt(input["limit"])

	if offset > 0 || limit > 0 {
		switch {
		case offset > 0 && limit > 0:
			filePath += fmt.Sprintf(" [%d:%d]", offset, offset+limit)
		case offset > 0:
			filePath += fmt.Sprintf(" [%d:]", offset)
		default:
			filePath += fmt.Sprintf(" [:%d]", limit)
		}
	}

	if len(filePath) > 120 {
		filePath = filePath[:120] + "…"
	}
	return filePath
}

// toInt converts a JSON number (float64) to int, returning 0 for non-numeric values.
func toInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	}
	return 0
}
