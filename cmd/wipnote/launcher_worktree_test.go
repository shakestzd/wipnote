package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestLauncherEnv_WorktreeCanonicalRoot verifies that canonicalProjectRoot
// returns the main repo root (not the worktree path) when CWD is a linked
// git worktree. It constructs a real git repo + linked worktree under TMPDIR.
func TestLauncherEnv_WorktreeCanonicalRoot(t *testing.T) {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = t.TempDir()
	}

	// Create a throwaway base dir under TMPDIR.
	base, err := os.MkdirTemp(tmpDir, "wipnote-wt-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })

	mainRepo := filepath.Join(base, "main")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("MkdirAll mainRepo: %v", err)
	}

	// Init main repo.
	mustGit(t, mainRepo, "init")
	mustGit(t, mainRepo, "config", "user.email", "test@example.com")
	mustGit(t, mainRepo, "config", "user.name", "Test")

	// Create .wipnote/ so ResolveViaGitCommonDir validates the candidate.
	if err := os.MkdirAll(filepath.Join(mainRepo, ".wipnote"), 0755); err != nil {
		t.Fatalf("MkdirAll .wipnote: %v", err)
	}
	// Need at least one commit for git worktree add to work.
	sentinel := filepath.Join(mainRepo, "README")
	if err := os.WriteFile(sentinel, []byte("wipnote test\n"), 0644); err != nil {
		t.Fatalf("WriteFile README: %v", err)
	}
	mustGit(t, mainRepo, "add", "README")
	mustGit(t, mainRepo, "commit", "-m", "init")

	// Create a linked worktree.
	worktreePath := filepath.Join(base, "worktree")
	mustGit(t, mainRepo, "worktree", "add", worktreePath, "-b", "wt-branch")

	// canonicalProjectRoot with main repo root should return "" (not a linked worktree).
	if got := canonicalProjectRoot(mainRepo); got != "" {
		t.Errorf("canonicalProjectRoot(mainRepo) = %q, want \"\" (main repo is not a linked worktree)", got)
	}

	// canonicalProjectRoot with linked worktree path should return the main repo root.
	got := canonicalProjectRoot(worktreePath)
	if got == "" {
		t.Fatalf("canonicalProjectRoot(worktreePath) returned \"\", want %q", mainRepo)
	}
	// Resolve symlinks for reliable comparison.
	wantResolved, _ := filepath.EvalSymlinks(mainRepo)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Errorf("canonicalProjectRoot(worktreePath) = %q (resolved: %q), want %q (resolved: %q)",
			got, gotResolved, mainRepo, wantResolved)
	}
}

// TestLauncherEnv_MainRepoNoOverride verifies that when CWD is the main
// repo (not a linked worktree), canonicalProjectRoot returns "" so callers
// do not inject a redundant WIPNOTE_PROJECT_DIR override.
func TestLauncherEnv_MainRepoNoOverride(t *testing.T) {
	tmpDir := os.Getenv("TMPDIR")
	if tmpDir == "" {
		tmpDir = t.TempDir()
	}

	base, err := os.MkdirTemp(tmpDir, "wipnote-main-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })

	mainRepo := filepath.Join(base, "main")
	if err := os.MkdirAll(mainRepo, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustGit(t, mainRepo, "init")
	mustGit(t, mainRepo, "config", "user.email", "test@example.com")
	mustGit(t, mainRepo, "config", "user.name", "Test")
	if err := os.MkdirAll(filepath.Join(mainRepo, ".wipnote"), 0755); err != nil {
		t.Fatalf("MkdirAll .wipnote: %v", err)
	}

	// Main repo: not a linked worktree — canonicalProjectRoot must return "".
	if got := canonicalProjectRoot(mainRepo); got != "" {
		t.Errorf("canonicalProjectRoot(mainRepo) = %q, want \"\" for main worktree", got)
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}
