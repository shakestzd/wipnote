package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/htmlparse"
)

// TestBugStart_CodexSessionIgnoresStaleClaudeActiveSession is the issue #148
// regression: inside a Codex task (CODEX_THREAD_ID set, no wipnote launcher
// stamp) a failed nested `wipnote claude` bootstrap left a Claude-tagged
// .wipnote/.active-session behind. `wipnote bug start` must claim the Codex
// session — the one the PreToolUse hook enforces against — and `wipnote who`
// must report the same id via the same resolver.
func TestBugStart_CodexSessionIgnoresStaleClaudeActiveSession(t *testing.T) {
	if testing.Short() {
		t.Skip("drives full work-item start lifecycle")
	}
	const (
		codexThread = "019f0000-0000-7000-8000-000000000148"
		staleClaude = "cf3d0000-0000-4000-8000-000000000148"
	)

	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		os.MkdirAll(filepath.Join(hgDir, sub), 0o755)
	}
	projectDirFlag = tmpDir
	defer func() { projectDirFlag = "" }()
	t.Chdir(tmpDir)

	// Isolate from the harness this test runner itself executes under, then
	// model a plugin-only Codex desktop task.
	for _, k := range []string{
		"WIPNOTE_HARNESS", "CLAUDE_CODE_ENTRYPOINT", "CLAUDECODE", "CLAUDE_CODE",
		"CLAUDE_CODE_SESSION_ID", "CLAUDE_SESSION_ID", "WIPNOTE_SESSION_ID",
		"GEMINI_SESSION_ID", "ANTIGRAVITY_SESSION_ID", "CLAUDE_PROJECT_DIR",
	} {
		t.Setenv(k, "")
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", tmpDir)
	t.Setenv("CODEX_THREAD_ID", codexThread)

	// The stale entry a failed nested `wipnote claude` left behind.
	hooks.WriteActiveSessionForHarness(staleClaude, tmpDir, "claude")

	trackID := testSetupTrack(t, hgDir)
	if err := testCreate("bug", "Attribution bug", trackID, "high", false, false); err != nil {
		t.Fatalf("create bug: %v", err)
	}
	bugFiles, _ := filepath.Glob(filepath.Join(hgDir, "bugs", "bug-*.html"))
	if len(bugFiles) != 1 {
		t.Fatalf("expected 1 bug file, got %d", len(bugFiles))
	}
	node, err := htmlparse.ParseFile(bugFiles[0])
	if err != nil {
		t.Fatalf("parse bug: %v", err)
	}

	if id, src := hooks.ResolveSessionID(""); id != codexThread || src != hooks.SessionSourceHarnessEnv {
		t.Fatalf("ResolveSessionID = (%q, %q), want (%q, %q)", id, src, codexThread, hooks.SessionSourceHarnessEnv)
	}

	if err := runWiSetStatus("bug", node.ID, "in-progress"); err != nil {
		t.Fatalf("bug start: %v", err)
	}
	node, _ = htmlparse.ParseFile(bugFiles[0])
	impl := node.Edges["implemented_in"]
	if len(impl) == 0 {
		t.Fatalf("bug missing implemented_in edge; edges = %v", node.Edges)
	}
	if impl[0].TargetID != codexThread {
		t.Fatalf("bug start claimed session %q, want the Codex thread %q (stale Claude entry %q must be ignored)",
			impl[0].TargetID, codexThread, staleClaude)
	}
}
