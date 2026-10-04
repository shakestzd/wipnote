package hooks

import (
	"fmt"
	"io"
	"time"
)

// timestampLayout mirrors the retired shell hook
// `date '+Current time: %Y-%m-%dT%H:%M:%S%z (%Z)'`.
const timestampLayout = "2006-01-02T15:04:05-0700 (MST)"

// TimestampLine returns the "Current time: ..." line previously produced by a
// separate `date` shell hook on every UserPromptSubmit.
func TimestampLine(now time.Time) string {
	return "Current time: " + now.Format(timestampLayout)
}

// EmitClaudeUserPrompt writes the Claude Code UserPromptSubmit response with
// the current timestamp folded in, replacing the standalone `date` hook (one
// fewer process per prompt).
//
// Claude Code adds plain (non-JSON) stdout from a UserPromptSubmit hook to the
// model's context. For a context-only result (nothing but AdditionalContext, or
// nothing at all) the timestamp and any guidance are therefore emitted as plain
// text, which does not depend on the JSON hookSpecificOutput shape. Any result
// that carries a decision, reason, message or structured output must stay JSON;
// there the timestamp is prepended to AdditionalContext instead.
func EmitClaudeUserPrompt(w io.Writer, result *HookResult, now time.Time) error {
	ts := TimestampLine(now)
	if result == nil {
		result = &HookResult{}
	}
	if result.Decision == "" && result.Reason == "" && result.Message == "" &&
		result.HookSpecificOutput == nil {
		out := ts + "\n"
		if result.AdditionalContext != "" {
			out += result.AdditionalContext + "\n"
		}
		_, err := fmt.Fprint(w, out)
		return err
	}
	r := *result
	if r.AdditionalContext != "" {
		r.AdditionalContext = ts + "\n" + r.AdditionalContext
	} else {
		r.AdditionalContext = ts
	}
	return emitClaudeResponse(w, &r)
}
