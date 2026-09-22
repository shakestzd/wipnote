package agent_test

import (
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

// clearHarnessEnv isolates a test from the ambient harness the test runner
// itself is executing under (a Claude Code Bash tool exports CLAUDECODE,
// CLAUDE_CODE_ENTRYPOINT and CLAUDE_CODE_SESSION_ID).
func clearHarnessEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"WIPNOTE_HARNESS", "CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE", "CLAUDE_CODE",
		"CODEX_THREAD_ID", "GEMINI_SESSION_ID", "ANTIGRAVITY_SESSION_ID",
		"CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID", "WIPNOTE_SESSION_ID",
	} {
		t.Setenv(k, "")
	}
}

// TestDetectEnvHarness pins the caller-harness inference EnvSessionID uses to
// decide whether an .active-session entry may be adopted (issue #148): the
// launcher stamp wins, then Claude's own markers, then the harness-native id
// variables a plugin-only session exposes.
func TestDetectEnvHarness(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "nothing set: unknown", want: ""},
		{name: "WIPNOTE_HARNESS=codex wins over inherited Claude markers",
			env: map[string]string{"WIPNOTE_HARNESS": "codex", "CLAUDECODE": "1", "CLAUDE_CODE_ENTRYPOINT": "cli"}, want: "codex"},
		{name: "WIPNOTE_HARNESS=claude wins over inherited CODEX_THREAD_ID (nested launch)",
			env: map[string]string{"WIPNOTE_HARNESS": "claude", "CODEX_THREAD_ID": "019f-thread"}, want: "claude"},
		{name: "WIPNOTE_HARNESS is normalised", env: map[string]string{"WIPNOTE_HARNESS": " Codex "}, want: "codex"},
		{name: "unknown WIPNOTE_HARNESS falls through to markers",
			env: map[string]string{"WIPNOTE_HARNESS": "bogus", "CODEX_THREAD_ID": "019f-thread"}, want: "codex"},
		{name: "CLAUDE_CODE_ENTRYPOINT alone: claude", env: map[string]string{"CLAUDE_CODE_ENTRYPOINT": "cli"}, want: "claude"},
		{name: "CLAUDECODE alone: claude", env: map[string]string{"CLAUDECODE": "1"}, want: "claude"},
		{name: "Claude markers beat CODEX_THREAD_ID (bare claude inside a Codex task)",
			env: map[string]string{"CLAUDECODE": "1", "CODEX_THREAD_ID": "019f-thread"}, want: "claude"},
		{name: "CODEX_THREAD_ID alone: codex (plugin-only desktop task)",
			env: map[string]string{"CODEX_THREAD_ID": "019f-thread"}, want: "codex"},
		{name: "whitespace CODEX_THREAD_ID is not a marker", env: map[string]string{"CODEX_THREAD_ID": "  "}, want: ""},
		{name: "GEMINI_SESSION_ID alone: gemini", env: map[string]string{"GEMINI_SESSION_ID": "g-1"}, want: "gemini"},
		{name: "ANTIGRAVITY_SESSION_ID alone: antigravity", env: map[string]string{"ANTIGRAVITY_SESSION_ID": "a-1"}, want: "antigravity"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			clearHarnessEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := agent.DetectEnvHarness(); got != tc.want {
				t.Errorf("DetectEnvHarness() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestHarnessNativeEnvSessionID_InferredCodex verifies a Codex session with no
// launcher stamp still resolves its own thread id (issue #148 environment).
func TestHarnessNativeEnvSessionID_InferredCodex(t *testing.T) {
	clearHarnessEnv(t)
	t.Setenv("CODEX_THREAD_ID", "019f-codex-thread")
	if got := agent.HarnessNativeEnvSessionID(); got != "019f-codex-thread" {
		t.Fatalf("HarnessNativeEnvSessionID() = %q, want CODEX_THREAD_ID", got)
	}
	// A Claude marker makes the inherited thread id irrelevant.
	t.Setenv("CLAUDECODE", "1")
	if got := agent.HarnessNativeEnvSessionID(); got != "" {
		t.Fatalf("HarnessNativeEnvSessionID() under Claude = %q, want empty", got)
	}
}
