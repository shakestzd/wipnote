package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const lateRowSessionID = "019fe7af-9ba5-7e60-b288-656d6fe72e70"

// clearLateRowEnv removes every variable agent.Detect / DetectEnvHarness read,
// so the case under test sets exactly the markers it means.
func clearLateRowEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"WIPNOTE_AGENT_ID", "WIPNOTE_HARNESS", "CLAUDECODE", "CLAUDE_CODE",
		"CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID", "CODEX_SESSION_ID",
		"GEMINI_SESSION_ID", "ANTIGRAVITY_SESSION_ID", "CLAUDE_SESSION_ID",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("WIPNOTE_SESSION_ID", lateRowSessionID)
}

func ensureLateRow(t *testing.T) (sessionledger.Record, bool) {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.Mkdir(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	if _, err := agent.EnsureSession(nil, projectDir); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	rec, ok, err := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote")).Get(lateRowSessionID)
	if err != nil {
		t.Fatalf("ledger Get: %v", err)
	}
	return rec, ok
}

// TestEnsureSessionLateRowWithUnknownHarnessIsLeftOut: a CLI command run where
// no harness marks the process (a plain shell, a delegate script) must not
// create the session's row from the "human" agent fallback (bug-5333ea49).
func TestEnsureSessionLateRowWithUnknownHarnessIsLeftOut(t *testing.T) {
	clearLateRowEnv(t)
	if rec, ok := ensureLateRow(t); ok {
		t.Fatalf("late row written with harness %q; an unknown harness must leave the row out", rec.Harness)
	}
}

// TestEnsureSessionLateRowTakesTheKnownHarness: inside a harness the late row
// carries that harness's tag — the same one SessionStart writes — never the
// agent identity.
func TestEnsureSessionLateRowTakesTheKnownHarness(t *testing.T) {
	cases := map[string][2]string{
		"claude bash tool": {"CLAUDECODE", "1"},
		"codex launcher":   {"WIPNOTE_HARNESS", "codex"},
	}
	want := map[string]string{"claude bash tool": "claude", "codex launcher": "codex"}
	for name, kv := range cases {
		t.Run(name, func(t *testing.T) {
			clearLateRowEnv(t)
			t.Setenv(kv[0], kv[1])
			rec, ok := ensureLateRow(t)
			if !ok {
				t.Fatal("no late row written inside a known harness")
			}
			if rec.Harness != want[name] {
				t.Errorf("harness = %q, want %q", rec.Harness, want[name])
			}
		})
	}
}
