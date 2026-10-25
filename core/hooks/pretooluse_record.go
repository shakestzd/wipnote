package hooks

import (
	"encoding/json"
	"fmt"
)

// recordEventAndAllow records the tool call in the canonical tool log and
// returns an allow result. Used by the plan mode bypass and the normal
// flow to avoid duplicating the event recording logic.
func recordEventAndAllow(event *CloudEvent, ctx *toolUseContext) (*HookResult, error) {
	inputSummary := SummariseInput(event.ToolName, event.ToolInput)

	// Normalize path fields to repo-relative before storage so that captured
	// artifacts remain stable across machines and worktrees. Bash "command"
	// fields are deliberately excluded (see normalizeToolInputPaths).
	toolInputStr := marshalToolInput(event.ToolName, event.ToolInput)

	// The canonical record of the tool call (bug-a3b17225): the guards, the
	// Stop event count, the session-end file pointers and the orphan sweep
	// all read it. Best-effort and bounded.
	appendCanonicalToolEvent(ctx.ProjectDir, ctx.SessionID, canonicalToolEvent{
		Tool:    event.ToolName,
		Agent:   ctx.AgentID,
		Summary: inputSummary,
		Input:   toolInputStr,
		ID:      toolCallEventID(ctx.SessionID, event.ToolUseID),
		TS:      toolLogNow().Unix(),
	})

	result := &HookResult{}
	// bug-190950e0: this is the single choke point every "allow" exit of
	// PreToolUse passes through, so patching it here (rather than each call
	// site) covers plan-mode bypass and the plain allow path uniformly, without touching any block/gate decision
	// above — this only ever runs once a tool call has already been allowed.
	applyClaimAgentPropagation(event, result)
	return result, nil
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
