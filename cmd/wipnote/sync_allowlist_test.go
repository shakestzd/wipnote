package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDirtyWipnoteFiles_Allowlist verifies sync only surfaces canonical store
// paths and never session telemetry (feat-9457496e).
func TestDirtyWipnoteFiles_Allowlist(t *testing.T) {
	cases := map[string]bool{
		".wipnote/features/feat-1.html":       true,
		".wipnote/bugs/bug-1.html":            true,
		".wipnote/spikes/spk-1.html":          true,
		".wipnote/plans/plan-1.yaml":          true,
		".wipnote/tracks/trk-1.html":          true,
		".wipnote/specs/s.html":               true,
		".wipnote/research/r.md":              true,
		".wipnote/archive/2026-05/x.tar.gz":   true,
		".wipnote/recaps/r.html":              true,
		".wipnote/claims/c.html":              true,
		".wipnote/runs/r.json":                true,
		".wipnote/migrations/m.json":          true,
		".wipnote/migrations/ai-title.done":   false,
		".wipnote/architecture.html":          true,
		".wipnote/gate-ledger.html":           true,
		".wipnote/sessions-ledger.html":       true,
		".wipnote/guard-profile.yaml":         true,
		".wipnote/agents.json":                true,
		".wipnote/.gitignore":                 true,
		".wipnote/sessions/abc/events.ndjson": false,
		".wipnote/session-families.json":      false,
		".wipnote/architecture.html.lock":     false,
		".wipnote/logs/x.log":                 false,
		".wipnote/.pre-merge-backup-1/a.html": false,
		".wipnote/gate-ledger.html.lock":      false,
		"other/features/x.html":               false,
	}
	for path, want := range cases {
		if got := isSyncAllowedPath(path); got != want {
			t.Errorf("isSyncAllowedPath(%q) = %v, want %v", path, got, want)
		}
	}

	tmp, err := os.MkdirTemp("/tmp", "wipnote-sync-allow-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	repo := setupWorktreeGitRepoIn(t, tmp)
	wd := filepath.Join(repo, ".wipnote")
	seedWipnoteCommit(t, repo)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(wd, "features"), 0o755))
	must(os.MkdirAll(filepath.Join(wd, "sessions", "s1"), 0o755))
	must(os.WriteFile(filepath.Join(wd, "sessions", "s1", "e.ndjson"), []byte("x"), 0o644))
	must(os.WriteFile(filepath.Join(wd, "features", "feat-al1.html"), []byte("<article/>"), 0o644))
	files, err := dirtyWipnoteFiles(repo)
	must(err)
	if len(files) != 1 || files[0] != ".wipnote/features/feat-al1.html" {
		t.Errorf("dirtyWipnoteFiles = %v, want only the feature", files)
	}
}
