package hooks

import (
	"database/sql"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/ingest"
	"github.com/shakestzd/wipnote/core/models"
)

// toolCallEventID is the ONE event_id scheme shared by the PreToolUse started
// row and the PostToolUse completion for the same logical tool call
// (bug-07fd4a51). It is ingest.EventID keyed on (session_id, tool_use_id), so
// the hook rows, the session-HTML <li data-event-id>, and transcript ingest
// (cmd/wipnote/ingest.go, RenderIngestedSessionHTML) all agree on the id.
//
// Pairing is by tool_use_id and nothing else: two parallel calls to the same
// tool carry different tool_use_ids and so land on different rows, which the
// old "newest started row with this tool name" lookup could not guarantee.
//
// Returns "" when either input is empty (Antigravity payloads carry no
// tool_use_id); callers mint a random id and the pre/post halves stay unpaired.
func toolCallEventID(sessionID, toolUseID string) string {
	if sessionID == "" || toolUseID == "" {
		return ""
	}
	return ingest.EventID(sessionID, toolUseID, "", 0)
}

// toolCallEventIDOrRandom is toolCallEventID with a random fallback for
// payloads that carry no tool_use_id.
func toolCallEventIDOrRandom(sessionID, toolUseID string) string {
	if id := toolCallEventID(sessionID, toolUseID); id != "" {
		return id
	}
	return uuid.New().String()
}

// marshalToolInput renders tool_input as the repo-relative, path-normalised JSON
// string stored in agent_events.tool_input (shared by pre and post).
func marshalToolInput(toolName string, input map[string]any) string {
	if input == nil {
		return ""
	}
	b, err := json.Marshal(input)
	if err != nil {
		return ""
	}
	return normalizeToolInputJSON(string(b), toolName)
}

// durationMS is the optional PostToolUse "duration_ms" field. It tolerates a
// JSON number (integer or float) or a numeric string; anything else decodes to
// zero rather than failing the whole payload, because a bad optional field must
// never cost the hook its event.
type durationMS int64

func (d *durationMS) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		*d = 0
		return nil
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil && f > 0 {
		*d = durationMS(f)
		return nil
	}
	*d = 0
	return nil
}

// Duration returns the value as a time.Duration (zero when absent).
func (d durationMS) Duration() time.Duration {
	return time.Duration(d) * time.Millisecond
}

// agentEventCompletionUpsertSQL writes the COMPLETE tool_call row from
// PostToolUse. It is an upsert on event_id so that when PreToolUse already
// wrote the started row under the same toolCallEventID the row is completed in
// place, and when it did not (the started row went to a store this write does
// not reach, or never happened) a self-contained row is created.
//
// parent_agent_id / parent_event_id are resolved by scalar subqueries in the
// store that APPLIES the statement (the daemon's projection in production), so
// a parent id the target store does not hold binds NULL instead of tripping the
// parent_event_id foreign key and losing the row. The conflict branch only
// fills lineage the started row lacks, keeps the started row's timestamp, and
// is guarded on session_id so a 32-bit evt- id collision with another
// session's row can never overwrite it.
const agentEventCompletionUpsertSQL = `
	INSERT INTO agent_events (
		event_id, agent_id, event_type, timestamp, tool_name,
		input_summary, tool_input, output_summary, session_id, feature_id,
		parent_agent_id, parent_event_id, subagent_type,
		cost_tokens, execution_duration_seconds, status,
		source, step_id, created_at, updated_at
	) VALUES (?,?,?,?,?, ?,?,?,?,?,
		(SELECT agent_id FROM agent_events WHERE event_id = ?),
		(SELECT event_id FROM agent_events WHERE event_id = ?),
		?, 0, ?, ?, 'hook', ?, ?, ?)
	ON CONFLICT(event_id) DO UPDATE SET
		status = excluded.status,
		output_summary = excluded.output_summary,
		execution_duration_seconds = excluded.execution_duration_seconds,
		feature_id = COALESCE(agent_events.feature_id, excluded.feature_id),
		parent_agent_id = COALESCE(agent_events.parent_agent_id, excluded.parent_agent_id),
		parent_event_id = COALESCE(agent_events.parent_event_id, excluded.parent_event_id),
		step_id = COALESCE(agent_events.step_id, excluded.step_id),
		updated_at = excluded.updated_at
	WHERE agent_events.session_id = excluded.session_id`

// postToolUseParentEventID returns the parent UserQuery event id for the
// completion row. PreToolUse persists it as WIPNOTE_PARENT_PROMPT_EVENT at the
// moment the tool starts (writeParentPromptEvent); PostToolUse takes that value
// verbatim rather than resolveParentEventID's DB-validated copy, because the
// hook's own handle never holds the UserQuery row in production. FK safety is
// provided by the subquery in agentEventCompletionUpsertSQL instead.
func postToolUseParentEventID(ctx *toolUseContext) string {
	if v := os.Getenv("WIPNOTE_PARENT_PROMPT_EVENT"); v != "" {
		return v
	}
	return ctx.ParentEventID
}

// buildToolCompletionEvent assembles the completed/failed tool_call row.
// Start time is now minus duration_ms when the payload carries it, else now.
func buildToolCompletionEvent(event *CloudEvent, ctx *toolUseContext, status, outputSummary string, now time.Time) *models.AgentEvent {
	dur := event.DurationMS.Duration()
	start := now
	if dur > 0 {
		start = now.Add(-dur)
	}
	return &models.AgentEvent{
		EventID:       toolCallEventIDOrRandom(ctx.SessionID, event.ToolUseID),
		AgentID:       ctx.AgentID,
		EventType:     models.EventToolCall,
		Timestamp:     start,
		ToolName:      event.ToolName,
		InputSummary:  SummariseInput(event.ToolName, event.ToolInput),
		ToolInput:     marshalToolInput(event.ToolName, event.ToolInput),
		OutputSummary: outputSummary,
		SessionID:     ctx.SessionID,
		FeatureID:     ctx.FeatureID,
		ParentEventID: postToolUseParentEventID(ctx),
		SubagentType:  ctx.AgentType,
		ExecDuration:  dur.Seconds(),
		Status:        status,
		StepID:        event.ToolUseID,
		Source:        "hook",
		CreatedAt:     start,
		UpdatedAt:     now,
	}
}

// toolCompletionArgs binds ev to agentEventCompletionUpsertSQL's placeholders.
func toolCompletionArgs(ev *models.AgentEvent) []any {
	return []any{
		ev.EventID, ev.AgentID, string(ev.EventType),
		ev.Timestamp.UTC().Format(time.RFC3339), nullableArg(ev.ToolName),
		nullableArg(ev.InputSummary), nullableArg(ev.ToolInput), nullableArg(ev.OutputSummary),
		ev.SessionID, nullableArg(ev.FeatureID),
		nullableArg(ev.ParentEventID), nullableArg(ev.ParentEventID),
		nullableArg(ev.SubagentType),
		ev.ExecDuration, ev.Status,
		nullableArg(ev.StepID),
		ev.CreatedAt.UTC().Format(time.RFC3339),
		ev.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// recordToolCompletion writes the complete PostToolUse event row through the
// daemon-first route (routeHookWriteVia: enqueue to the writer daemon; on a
// daemon miss, the bounded fallback against a writable handle). It returns the
// event id so the session HTML entry is keyed identically.
func recordToolCompletion(event *CloudEvent, ctx *toolUseContext, database *sql.DB, status, outputSummary string) string {
	ev := buildToolCompletionEvent(event, ctx, status, outputSummary, time.Now().UTC())
	_ = routeHookWriteVia("posttooluse", ctx.ProjectDir, ctx.SessionID, database,
		agentEventCompletionUpsertSQL, toolCompletionArgs(ev)...)
	return ev.EventID
}
