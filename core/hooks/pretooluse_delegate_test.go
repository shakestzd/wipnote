package hooks

import (
	"bytes"
	"encoding/json"
	"testing"
)

// These tests pin the hook half of the bare-delegation redirect with a stub
// recogniser; the real recogniser (internal/delegate/detect) lives in the root
// module and is tested through this hook there.

// stubRedirect installs fn as DelegateRedirect for one test.
func stubRedirect(t *testing.T, fn func(string) string) {
	t.Helper()
	prev := DelegateRedirect
	DelegateRedirect = fn
	t.Cleanup(func() { DelegateRedirect = prev })
}

func redirectEvent(tool, cmd string) *CloudEvent {
	return &CloudEvent{
		AgentID: "claude-code", SessionID: "test-sess", ToolName: tool,
		ToolInput: map[string]any{"command": cmd}, ToolUseID: "delegate-redirect",
	}
}

const stubReason = "use wipnote delegate run -- codex exec x"

func onlyCodexExec(cmd string) string {
	if cmd == "codex exec x" {
		return stubReason
	}
	return ""
}

// TestPreToolUse_DelegateRedirectDeniesThroughPipeline: the redirect is a
// guard of PreToolUse, and its block renders as permissionDecision deny on
// both Claude Code and Codex.
func TestPreToolUse_DelegateRedirectDeniesThroughPipeline(t *testing.T) {
	setupTestDB(t)
	t.Setenv("WIPNOTE_SESSION_ID", "test-sess")
	stubRedirect(t, onlyCodexExec)
	ev := redirectEvent("Bash", "codex exec x")
	ev.CWD = t.TempDir()
	res, err := PreToolUse(ev)
	if err != nil || res == nil || res.Decision != "block" || res.Reason != stubReason {
		t.Fatalf("PreToolUse = %+v, %v; want block with the recogniser's reason", res, err)
	}
	for name, emit := range map[string]func(*bytes.Buffer) error{
		"claude": func(b *bytes.Buffer) error { return emitClaudeResponseForEvent(b, "PreToolUse", res) },
		"codex":  func(b *bytes.Buffer) error { return emitCodexResponseForEvent(b, "PreToolUse", res) },
	} {
		var b bytes.Buffer
		if err := emit(&b); err != nil {
			t.Fatalf("%s emit: %v", name, err)
		}
		var wire struct {
			HSO struct {
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
				UpdatedInput             any    `json:"updatedInput"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(b.Bytes(), &wire); err != nil {
			t.Fatalf("%s wire %q: %v", name, b.String(), err)
		}
		if wire.HSO.PermissionDecision != "deny" || wire.HSO.PermissionDecisionReason != stubReason || wire.HSO.UpdatedInput != nil {
			t.Errorf("%s wire = %s, want deny with the reason and no updatedInput", name, b.String())
		}
	}
}

// TestDelegateRedirect_NeverAllowsOrRewrites: the result is nil or a block,
// never allow, never a hookSpecificOutput (so never updatedInput).
func TestDelegateRedirect_NeverAllowsOrRewrites(t *testing.T) {
	stubRedirect(t, onlyCodexExec)
	for _, cmd := range []string{"codex exec x", "ls", "codex exec y", ""} {
		for _, tool := range []string{"Bash", "exec_command", "functions.exec_command"} {
			res := DelegateRedirectResult(redirectEvent(tool, cmd))
			if res == nil {
				if cmd == "codex exec x" {
					t.Errorf("%s %q: not denied", tool, cmd)
				}
				continue
			}
			if res.Decision != "block" || res.HookSpecificOutput != nil || res.Reason == "" {
				t.Errorf("%s %q: result %+v, want a bare block with a reason", tool, cmd, res)
			}
		}
	}
}

// TestDelegateRedirect_FailsOpen: no recogniser, a non-shell tool, a missing
// command or a panicking recogniser all let the call through.
func TestDelegateRedirect_FailsOpen(t *testing.T) {
	stubRedirect(t, nil)
	if res := DelegateRedirectResult(redirectEvent("Bash", "codex exec x")); res != nil {
		t.Errorf("nil recogniser: %+v", res)
	}
	called := false
	stubRedirect(t, func(string) string { called = true; return "deny" })
	if res := DelegateRedirectResult(redirectEvent("Write", "codex exec x")); res != nil || called {
		t.Errorf("non-shell tool: %+v, called=%v", res, called)
	}
	if res := DelegateRedirectResult(&CloudEvent{ToolName: "Bash"}); res != nil {
		t.Errorf("no command: %+v", res)
	}
	if res := DelegateRedirectResult(nil); res != nil {
		t.Errorf("nil event: %+v", res)
	}
	stubRedirect(t, func(string) string { panic("boom") })
	if res := DelegateRedirectResult(redirectEvent("Bash", "codex exec x")); res != nil {
		t.Errorf("panicking recogniser: %+v", res)
	}
}
