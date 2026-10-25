package hooks

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/ingest"
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
