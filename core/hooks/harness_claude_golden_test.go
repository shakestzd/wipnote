package hooks

import (
	"bytes"
	"testing"
)

// TestEmitClaudeResponseForEventGolden pins the exact Claude Code wire format
// per event (verified live against `claude -p`; docs:
// https://code.claude.com/docs/en/hooks).
func TestEmitClaudeResponseForEventGolden(t *testing.T) {
	cases := []struct {
		name   string
		event  string
		result *HookResult
		want   string
	}{
		{"empty", "PreToolUse", &HookResult{}, `{}`},
		{"empty no event", "", &HookResult{}, `{}`},
		{"SessionStart context", "SessionStart", &HookResult{AdditionalContext: "hi"},
			`{"hookSpecificOutput":{"hookEventName":"SessionStart","additionalContext":"hi"}}`},
		{"UserPromptSubmit context", "UserPromptSubmit", &HookResult{AdditionalContext: "hi"},
			`{"hookSpecificOutput":{"hookEventName":"UserPromptSubmit","additionalContext":"hi"}}`},
		{"UserPromptSubmit block keeps top-level", "UserPromptSubmit", &HookResult{Decision: "block", Reason: "no"},
			`{"decision":"block","reason":"no"}`},
		{"PreToolUse context only", "PreToolUse", &HookResult{AdditionalContext: "advice"},
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"advice"}}`},
		{"PreToolUse deny", "PreToolUse", &HookResult{Decision: "block", Reason: "nope"},
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"nope"}}`},
		{"PreToolUse deny with context", "PreToolUse", &HookResult{Decision: "deny", Reason: "nope", AdditionalContext: "c"},
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","additionalContext":"c","permissionDecision":"deny","permissionDecisionReason":"nope"}}`},
		{"PreToolUse updatedInput preserved", "PreToolUse",
			&HookResult{HookSpecificOutput: &HookSpecificOutput{HookEventName: "PreToolUse", UpdatedInput: map[string]any{"command": "ls"}}},
			`{"hookSpecificOutput":{"hookEventName":"PreToolUse","updatedInput":{"command":"ls"}}}`},
		{"PostToolUse context", "PostToolUse", &HookResult{AdditionalContext: "warn"},
			`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"warn"}}`},
		{"Stop block", "Stop", &HookResult{Decision: "block", Reason: "keep going"},
			`{"decision":"block","reason":"keep going"}`},
		{"SubagentStop block", "SubagentStop", &HookResult{Decision: "block", Reason: "r"},
			`{"decision":"block","reason":"r"}`},
		{"PermissionRequest allow unchanged", "PermissionRequest",
			&HookResult{HookSpecificOutput: &HookSpecificOutput{HookEventName: "PermissionRequest", Decision: &PermissionDecision{Behavior: "allow"}}},
			`{"hookSpecificOutput":{"hookEventName":"PermissionRequest","decision":{"behavior":"allow"}}}`},
		{"event from hookSpecificOutput wins", "",
			&HookResult{AdditionalContext: "x", HookSpecificOutput: &HookSpecificOutput{HookEventName: "PostToolUse"}},
			`{"hookSpecificOutput":{"hookEventName":"PostToolUse","additionalContext":"x"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := emitClaudeResponseForEvent(&buf, tc.event, tc.result); err != nil {
				t.Fatal(err)
			}
			got := string(bytes.TrimSpace(buf.Bytes()))
			if got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}
