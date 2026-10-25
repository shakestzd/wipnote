package hooks

import (
	"testing"
)

func clearSessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")
	t.Setenv("CLAUDE_ENV_FILE", "")
	t.Setenv("WIPNOTE_SESSION_ID", "")
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "")
	t.Setenv("WIPNOTE_HARNESS", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
}

func TestResolveHarness_FromEnv(t *testing.T) {
	cases := []struct {
		name             string
		env              string
		claudeEntrypoint string
		want             string
	}{
		{"claude accepted", "claude", "", "claude"},
		{"codex accepted", "codex", "", "codex"},
		{"gemini accepted", "gemini", "", "gemini"},
		{"antigravity accepted", "antigravity", "", "antigravity"},
		{"case-insensitive", "Claude", "", "claude"},
		{"trimmed", "  codex  ", "", "codex"},
		{"garbage rejected", "rogue-cli", "", ""},
		// Finding C: WIPNOTE_HARNESS unset but CLAUDE_CODE_ENTRYPOINT set → "claude".
		{"claude inferred from entrypoint", "", "hooks", "claude"},
		// WIPNOTE_HARNESS wins over CLAUDE_CODE_ENTRYPOINT when both set.
		{"wipnote_harness wins over entrypoint", "codex", "hooks", "codex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearSessionEnv(t)
			t.Setenv("WIPNOTE_HARNESS", tc.env)
			t.Setenv("CLAUDE_CODE_ENTRYPOINT", tc.claudeEntrypoint)
			if got := resolveHarness(); got != tc.want {
				t.Errorf("harness = %q, want %q", got, tc.want)
			}
		})
	}
}
