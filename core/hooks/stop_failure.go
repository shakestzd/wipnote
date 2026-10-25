package hooks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// looseString decodes a JSON string as-is and keeps any other JSON value as
// its compact text. StopFailure's `error` is a string in Claude Code, but the
// key is generic enough that another harness may send an object there, and a
// strict string field would fail the whole event decode.
type looseString string

func (s *looseString) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*s = ""
		return nil
	}
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = looseString(str)
		return nil
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, data); err != nil {
		*s = looseString(data)
		return nil
	}
	*s = looseString(buf.String())
	return nil
}

// StopFailure handles the StopFailure Claude Code hook event, which runs
// instead of Stop when a turn ends on an API error (rate limit, overload,
// auth, billing, ...). Claude Code ignores its output, so it only records the
// error as a failed entry in the session HTML activity log.
func StopFailure(event *CloudEvent) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		sessionID = EnvSessionID(event.SessionID)
	}
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	errType := string(event.StopError)
	if errType == "" {
		errType = "unknown"
	}
	summary := fmt.Sprintf("Turn ended on API error: %s", errType)
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	featureID := canonicalActiveWorkItem(projectDir, sessionID)
	now := time.Now().UTC()
	AppendEventToSessionHTML(projectDir, sessionID, SessionEvent{
		Timestamp: now,
		ToolName:  "StopFailure",
		Success:   false,
		EventID:   uuid.New().String(),
		FeatureID: featureID,
		Summary:   summary,
		Status:    "failed",
	})

	return &HookResult{Continue: true}, nil
}
