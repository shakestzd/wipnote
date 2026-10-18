package hooks

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"

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
// error event: the error type goes in input_summary, and the details and the
// rendered error text go in output_summary.
func StopFailure(event *CloudEvent, database *sql.DB) (*HookResult, error) {
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
	return recordSimpleEventWithOutput(models.EventError, "StopFailure", summary, detail, "failed", event, database)
}
