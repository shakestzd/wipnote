package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteActiveSessionForHarness_TagsHarness verifies the .active-session
// entry carries the writing harness (issue #148) and that an untagged legacy
// file still parses with an empty Harness.
func TestWriteActiveSessionForHarness_TagsHarness(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	WriteActiveSessionForHarness("sess-codex", projectDir, "codex")
	as := ReadActiveSession(projectDir)
	if as == nil || as.SessionID != "sess-codex" || as.Harness != "codex" {
		t.Fatalf("ReadActiveSession = %+v, want session_id=sess-codex harness=codex", as)
	}

	// An unknown harness string is dropped rather than written verbatim.
	WriteActiveSessionForHarness("sess-x", projectDir, "bogus")
	if as = ReadActiveSession(projectDir); as == nil || as.Harness != "" {
		t.Fatalf("unknown harness must normalise to empty, got %+v", as)
	}

	legacy := `{"session_id":"sess-legacy","nesting_depth":0,"timestamp":1}`
	if err := os.WriteFile(filepath.Join(projectDir, ".wipnote", ".active-session"), []byte(legacy), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if as = ReadActiveSession(projectDir); as == nil || as.SessionID != "sess-legacy" || as.Harness != "" {
		t.Fatalf("legacy file: got %+v", as)
	}
}

// TestSessionStart_TagsActiveSessionWithEventHarness verifies a Codex
// SessionStart writes a codex-tagged entry even when the process inherited a
// Claude marker from a parent shell.
func TestSessionStart_TagsActiveSessionWithEventHarness(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("WIPNOTE_HARNESS", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli") // inherited from a parent Claude shell
	if got := activeSessionHarness(&CloudEvent{Harness: HarnessCodex}); got != "codex" {
		t.Fatalf("activeSessionHarness(codex event) = %q, want codex", got)
	}
	t.Setenv("WIPNOTE_HARNESS", "claude") // launcher stamp is authoritative
	if got := activeSessionHarness(&CloudEvent{Harness: HarnessCodex}); got != "claude" {
		t.Fatalf("activeSessionHarness with launcher stamp = %q, want claude", got)
	}
	t.Setenv("WIPNOTE_HARNESS", "")
	if got := activeSessionHarness(nil); got != "claude" {
		t.Fatalf("activeSessionHarness(nil) = %q, want claude from CLAUDE_CODE_ENTRYPOINT", got)
	}
}
