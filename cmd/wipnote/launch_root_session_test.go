package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const (
	launchRootSessionID   = "aaaabbbb-cccc-dddd-eeee-ffff00002222"
	launchInheritedParent = "99998888-7777-6666-5555-444433331111"
)

// TestLauncherEnvMarksSessionRoot: every launcher env builder marks the child
// a ROOT session (WIPNOTE_NESTING_DEPTH=0) and keeps an inherited parent only
// as lineage (bug-5333ea49).
func TestLauncherEnvMarksSessionRoot(t *testing.T) {
	clearOtelEnv(t)
	t.Setenv("WIPNOTE_PARENT_SESSION", launchInheritedParent)
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")

	builders := map[string]func() []string{
		"claude":      func() []string { return buildClaudeLaunchEnv("", nil) },
		"codex":       func() []string { return withHarnessEnv(os.Environ(), harnessCodex) },
		"antigravity": func() []string { return withHarnessEnv(os.Environ(), harnessAntigravity) },
	}
	for name, build := range builders {
		env := build()
		if got := envValue(env, "WIPNOTE_NESTING_DEPTH"); got != "0" {
			t.Errorf("%s: WIPNOTE_NESTING_DEPTH = %q, want 0 (launcher-started sessions are root)", name, got)
		}
		if got := envValue(env, "WIPNOTE_PARENT_SESSION"); got != launchInheritedParent {
			t.Errorf("%s: WIPNOTE_PARENT_SESSION = %q, want the inherited parent kept as lineage", name, got)
		}
	}
}

// TestLauncherEnvTurnsOldTriggerIntoRootRow is the bug's trigger end to end:
// an inherited parent with no depth. Through the launcher env builder the
// SessionStart hook now writes the session's own claude ledger row.
func TestLauncherEnvTurnsOldTriggerIntoRootRow(t *testing.T) {
	clearOtelEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("CLAUDE_ENV_FILE", "")
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("WIPNOTE_PARENT_SESSION", launchInheritedParent)
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")

	t.Setenv("WIPNOTE_NESTING_DEPTH", envValue(buildClaudeLaunchEnv("", nil), "WIPNOTE_NESTING_DEPTH"))
	t.Setenv("WIPNOTE_PROJECT_DIR", "")

	event := &hooks.CloudEvent{SessionID: launchRootSessionID, CWD: projectDir}
	if _, err := hooks.SessionStart(event, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	recs, err := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote")).ReadAll()
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	if len(recs) != 1 || recs[0].SessionID != launchRootSessionID || recs[0].Harness != "claude" {
		t.Fatalf("ledger rows = %+v, want one claude row for %s", recs, launchRootSessionID)
	}
}
