package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateSessionEnv clears every variable ResolveSessionID and the harness
// inference consult, and pins the project dir to a fresh temp project.
func isolateSessionEnv(t *testing.T) string {
	t.Helper()
	for _, k := range []string{
		"WIPNOTE_HARNESS", "CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE", "CLAUDE_CODE",
		"CODEX_THREAD_ID", "GEMINI_SESSION_ID", "ANTIGRAVITY_SESSION_ID",
		"CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID", "WIPNOTE_SESSION_ID",
		"CLAUDE_PROJECT_DIR",
	} {
		t.Setenv(k, "")
	}
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Chdir(projectDir)
	return projectDir
}

// TestResolveSessionID_ActiveSessionHarnessFilter reproduces issue #148: a
// failed nested `wipnote claude` leaves a Claude-tagged .active-session behind
// in a Codex task. The Codex caller must never adopt it — it resolves its own
// CODEX_THREAD_ID instead — while same-harness, untagged (legacy) and
// unknown-caller entries keep the historical fallback behaviour.
func TestResolveSessionID_ActiveSessionHarnessFilter(t *testing.T) {
	const (
		claudeStale = "cf3d0000-stale-claude-session"
		codexThread = "019f0000-codex-thread"
	)
	tests := []struct {
		name       string
		env        map[string]string
		entry      string // harness tag written to .active-session ("" = no file)
		legacyFile bool   // write the pre-tag JSON shape instead
		wantID     string
		wantSource string
	}{
		{
			name:       "codex task (plugin-only) + stale claude entry: claims its own thread",
			env:        map[string]string{"CODEX_THREAD_ID": codexThread},
			entry:      "claude",
			wantID:     codexThread,
			wantSource: SessionSourceHarnessEnv,
		},
		{
			name:       "codex launcher stamp + stale claude entry, no thread id: nothing, never the claude id",
			env:        map[string]string{"WIPNOTE_HARNESS": "codex"},
			entry:      "claude",
			wantID:     "",
			wantSource: SessionSourceNone,
		},
		{
			name:       "codex caller + codex entry: adopted from file",
			env:        map[string]string{"WIPNOTE_HARNESS": "codex"},
			entry:      "codex",
			wantID:     claudeStale,
			wantSource: SessionSourceActiveSession,
		},
		{
			name:       "claude caller + codex entry: not adopted",
			env:        map[string]string{"CLAUDECODE": "1"},
			entry:      "codex",
			wantID:     "",
			wantSource: SessionSourceNone,
		},
		{
			name:       "claude caller + untagged legacy file: adopted (backward compatible)",
			env:        map[string]string{"CLAUDECODE": "1"},
			legacyFile: true,
			wantID:     claudeStale,
			wantSource: SessionSourceActiveSession,
		},
		{
			name:       "unknown caller + claude entry: adopted (pre-tag behaviour)",
			entry:      "claude",
			wantID:     claudeStale,
			wantSource: SessionSourceActiveSession,
		},
		{
			name:       "WIPNOTE_SESSION_ID beats the file",
			env:        map[string]string{"WIPNOTE_SESSION_ID": "wip-env-session"},
			entry:      "claude",
			wantID:     "wip-env-session",
			wantSource: SessionSourceWipnoteEnv,
		},
		{
			name:       "no file, nothing set: empty",
			wantID:     "",
			wantSource: SessionSourceNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := isolateSessionEnv(t)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			switch {
			case tc.legacyFile:
				legacy := `{"session_id":"` + claudeStale + `","parent_session":"` + claudeStale + `","parent_agent":"claude-code","nesting_depth":0,"timestamp":1}`
				if err := os.WriteFile(filepath.Join(projectDir, ".wipnote", ".active-session"), []byte(legacy), 0o644); err != nil {
					t.Fatalf("write legacy file: %v", err)
				}
			case tc.entry != "":
				WriteActiveSessionForHarness(claudeStale, projectDir, tc.entry)
			}
			gotID, gotSource := ResolveSessionID("")
			if gotID != tc.wantID || gotSource != tc.wantSource {
				t.Fatalf("ResolveSessionID() = (%q, %q), want (%q, %q)", gotID, gotSource, tc.wantID, tc.wantSource)
			}
			if EnvSessionID("") != gotID {
				t.Fatalf("EnvSessionID diverged from ResolveSessionID")
			}
		})
	}
}

// TestResolveSessionID_PayloadWins pins the hook-path invariant and its label.
func TestResolveSessionID_PayloadWins(t *testing.T) {
	isolateSessionEnv(t)
	t.Setenv("CODEX_THREAD_ID", "019f-thread")
	id, src := ResolveSessionID("/mock/claude/-Users-x-/550e8400-e29b-41d4-a716-446655440000")
	if id != "550e8400-e29b-41d4-a716-446655440000" || src != SessionSourcePayload {
		t.Fatalf("ResolveSessionID(payload) = (%q, %q)", id, src)
	}
}

// TestResolveSessionID_ClaudeChildSessionPrefersHarnessID is the issue #125
// regression: in a Claude Code child session WIPNOTE_SESSION_ID (inherited
// from the parent's CLAUDE_ENV_FILE) differs from CLAUDE_CODE_SESSION_ID (the
// id the PreToolUse hook receives). The CLI must resolve the harness id so
// `feature start` claims under the session the gate checks.
func TestResolveSessionID_ClaudeChildSessionPrefersHarnessID(t *testing.T) {
	isolateSessionEnv(t)
	const (
		stale = "019ed551558d5a88c3826a7e66ad"
		live  = "bdc7f0a4-fcba-4068-b8db-ef515ac1e7c8"
	)
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_CHILD_SESSION", "1")
	t.Setenv("WIPNOTE_SESSION_ID", stale)
	t.Setenv("CLAUDE_CODE_SESSION_ID", live)
	id, src := ResolveSessionID("")
	if id != live || src != SessionSourceHarnessEnv {
		t.Fatalf("ResolveSessionID() = (%q, %q), want (%q, %q)", id, src, live, SessionSourceHarnessEnv)
	}
	// The hook path with a payload agrees with the CLI path.
	if hookID := EnvSessionID(live); hookID != id {
		t.Fatalf("hook resolved %q, CLI resolved %q — they must agree", hookID, id)
	}
	// Without the harness id, WIPNOTE_SESSION_ID still applies (unchanged).
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	if id, src = ResolveSessionID(""); id != stale || src != SessionSourceWipnoteEnv {
		t.Fatalf("fallback = (%q, %q), want (%q, %q)", id, src, stale, SessionSourceWipnoteEnv)
	}
}

func TestActiveSessionMatchesCaller(t *testing.T) {
	tests := []struct {
		entry, caller string
		want          bool
	}{
		{"", "", true},
		{"", "codex", true},
		{"claude", "", true},
		{"claude", "claude", true},
		{"claude", "codex", false},
		{"codex", "claude", false},
	}
	for _, tc := range tests {
		got := activeSessionMatchesCaller(&ActiveSessionData{Harness: tc.entry}, tc.caller)
		if got != tc.want {
			t.Errorf("activeSessionMatchesCaller(entry=%q, caller=%q) = %v, want %v", tc.entry, tc.caller, got, tc.want)
		}
	}
	if activeSessionMatchesCaller(nil, "") {
		t.Error("nil entry must not match")
	}
}
