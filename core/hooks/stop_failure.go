package hooks

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
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
// auth, billing, ...). Claude Code ignores its output, so it only records an
// error event: the error type in input_summary, and the details and the
// rendered error text in output_summary.
//
// Canonical first: the event goes to the session HTML activity log before the
// SQLite insert, under the same event id, so a reindex rebuilds it and a
// failed database open does not lose it.
func StopFailure(event *CloudEvent, database *sql.DB) (*HookResult, error) {
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
	detail := string(event.ErrorDetails)
	if msg := event.LastAssistantMessage; msg != "" {
		if detail != "" {
			detail += "; "
		}
		detail += msg
	}

	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	featureID := cachedGetActiveFeatureID(database, sessionID)
	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:       uuid.New().String(),
		AgentID:       resolveEventAgentID(event),
		EventType:     models.EventError,
		Timestamp:     now,
		ToolName:      "StopFailure",
		InputSummary:  summary,
		OutputSummary: detail,
		SessionID:     sessionID,
		FeatureID:     featureID,
		Status:        "failed",
		Source:        "hook",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	AppendEventToSessionHTML(projectDir, sessionID, SessionEvent{
		Timestamp: now,
		ToolName:  ev.ToolName,
		Success:   false,
		EventID:   ev.EventID,
		FeatureID: featureID,
		Summary:   summary,
		Status:    ev.Status,
	})

	if database != nil {
		if err := db.InsertEvent(database, ev); err != nil {
			debugLog(projectDir, "[error] handler=StopFailure session=%s: insert event: %v", sessionID[:minSessionLen(sessionID)], err)
		}
	}
	return &HookResult{Continue: true}, nil
}
