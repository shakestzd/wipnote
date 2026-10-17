package hooks

import (
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
// Every result goes through the one Claude emitter as JSON, with the timestamp
// prepended to AdditionalContext, so the hookSpecificOutput mapping and the
// per-event context cap (context_budget.go) apply to both the context-only and
// the decision case (issue #196). The earlier plain-stdout branch for
// context-only results bypassed the cap, and the JSON branch passed no event
// name, which put the context in the top-level additionalContext field Claude
// Code drops — so a decision result lost its timestamp and guidance.
func EmitClaudeUserPrompt(w io.Writer, result *HookResult, now time.Time) error {
	var r HookResult
	if result != nil {
		r = *result
	}
	ts := TimestampLine(now)
	if r.AdditionalContext != "" {
		r.AdditionalContext = ts + "\n" + r.AdditionalContext
	} else {
		r.AdditionalContext = ts
	}
	return emitClaudeResponseForEvent(w, "UserPromptSubmit", &r)
}
