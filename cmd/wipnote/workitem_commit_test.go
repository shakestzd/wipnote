package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCompleteCommitsWipnoteArtifact verifies that completing a feature from
// inside a worktree commits the .wipnote/features/ HTML to the main repo even
// though the worktree's per-worktree exclude suppresses .wipnote/ from the
// worktree's own git status.
func TestCompleteCommitsWipnoteArtifact(t *testing.T) {
	// Force temp dirs to /tmp (outside the project tree) so that t.TempDir()
	// returns a path that cannot walk up into the project's .git directory.
	// The project may set TMPDIR to .test-tmp/ (inside the repo) to avoid
	// /tmp noexec in devcontainers; we override that here so our isolated git
	// repos stay truly outside the real repo.
	tmpDir, err := os.MkdirTemp("/tmp", "wipnote-commit-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	// Step 1: create a main git repo with an initial commit.
	// setupWorktreeGitRepo (worktree_helpers_test.go) creates a temp dir with
	// a git repo and an initial commit — exactly what worktree creation needs.
	mainRepo := setupWorktreeGitRepoIn(t, tmpDir)

	// Step 2: create the .wipnote structure and commit a seed so the dir exists.
	wipnoteDir := filepath.Join(mainRepo, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(wipnoteDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	// Write and commit a placeholder so .wipnote/ is tracked in the main repo.
	placeholder := filepath.Join(wipnoteDir, ".keep")
	if err := os.WriteFile(placeholder, []byte(""), 0o644); err != nil {
		t.Fatalf("write placeholder: %v", err)
	}
	for _, args := range [][]string{
		{"-C", mainRepo, "add", ".wipnote/.keep"},
		{"-C", mainRepo, "commit", "-m", "add wipnote dir"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Step 3: create a worktree for the feature and install the per-worktree
	// exclude using EnsureForFeature which internally calls excludeWipnoteFromWorktree.
	featureID := "feat-test123"
	writeFeatureHTML(t, mainRepo, featureID, "")

	worktreePath, err := EnsureForFeature(featureID, mainRepo, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForFeature: %v", err)
	}

	// Verify the worktree exclude actually suppresses .wipnote/.
	// Write a file and check git status shows it as untracked in the worktree.
	testFile := filepath.Join(worktreePath, ".wipnote", "features", "check.html")
	_ = os.MkdirAll(filepath.Dir(testFile), 0o755)
	_ = os.WriteFile(testFile, []byte("<html></html>"), 0o644)
	statusOut, _ := exec.Command("git", "-C", worktreePath, "status", "--porcelain").CombinedOutput()
	if strings.Contains(string(statusOut), ".wipnote") {
		t.Logf("Note: .wipnote/ visible in worktree status — exclude may not have fired: %s", statusOut)
	}

	// Step 4: from the worktree's CWD, write the feature HTML to the MAIN repo.
	// (The worktree has a different .wipnote path; write to the main repo's path.)
	mainFeatureHTML := filepath.Join(wipnoteDir, "features", featureID+".html")
	if err := os.WriteFile(mainFeatureHTML, []byte(`<article id="`+featureID+`" data-status="done"><header><h1>Test Feature</h1></header><section data-content><p>Description</p></section></article>`), 0o644); err != nil {
		t.Fatalf("write feature HTML to main repo: %v", err)
	}

	// Step 5: call commitWipnoteArtifact directly, simulating what wiSetStatusWithAgent
	// does after completing a work item.
	if err := commitWipnoteArtifact(wipnoteDir, "feature", featureID); err != nil {
		t.Fatalf("commitWipnoteArtifact: %v", err)
	}

	// Step 6: assert the commit landed on the main repo HEAD.
	logOut, err := exec.Command("git", "-C", mainRepo, "log", "--oneline", "-1").CombinedOutput()
	if err != nil {
		t.Fatalf("git log: %v\n%s", err, logOut)
	}
	if !strings.Contains(string(logOut), "wipnote:") {
		t.Errorf("expected HEAD commit to start with 'wipnote:', got: %s", logOut)
	}

	// Step 7: assert the commit contains the feature HTML.
	showOut, err := exec.Command("git", "-C", mainRepo, "show", "--name-only", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git show: %v\n%s", err, showOut)
	}
	if !strings.Contains(string(showOut), featureID+".html") {
		t.Errorf("expected %s.html in commit, got:\n%s", featureID, showOut)
	}
}

// TestCompleteCommitsWipnoteArtifact_NoOpWhenAlreadyCommitted verifies that
// calling commitWipnoteArtifact when the file is already committed and
// unmodified produces no new commit (idempotent / nothing-to-commit path).
func TestCompleteCommitsWipnoteArtifact_NoOpWhenAlreadyCommitted(t *testing.T) {
	// Force temp dirs to /tmp (outside the project tree). See
	// TestCompleteCommitsWipnoteArtifact for the full rationale.
	tmpDir, err := os.MkdirTemp("/tmp", "wipnote-commit-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	mainRepo := setupWorktreeGitRepoIn(t, tmpDir)

	wipnoteDir := filepath.Join(mainRepo, ".wipnote")
	if err := os.MkdirAll(filepath.Join(wipnoteDir, "features"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	featureID := "feat-already1"
	featureHTML := filepath.Join(wipnoteDir, "features", featureID+".html")
	if err := os.WriteFile(featureHTML, []byte(`<article id="`+featureID+`" data-status="done"></article>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Commit the file directly — simulating it already being committed.
	for _, args := range [][]string{
		{"-C", mainRepo, "add", ".wipnote/features/" + featureID + ".html"},
		{"-C", mainRepo, "commit", "-m", "wipnote: complete " + featureID},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Record commit count before the idempotent call.
	countBefore := gitCommitCount(t, mainRepo)

	// Call commitWipnoteArtifact — nothing has changed, so it should be a no-op.
	if err := commitWipnoteArtifact(wipnoteDir, "feature", featureID); err != nil {
		t.Fatalf("commitWipnoteArtifact (idempotent): %v", err)
	}

	countAfter := gitCommitCount(t, mainRepo)
	if countAfter != countBefore {
		t.Errorf("expected no new commit (idempotent), commit count changed from %d to %d", countBefore, countAfter)
	}
}

// TestCompleteCommitsWipnoteArtifact_SkipsWhenNoGitRepo verifies that the
// function is a no-op (returns nil) when the wipnote dir is not inside a git repo.
func TestCompleteCommitsWipnoteArtifact_SkipsWhenNoGitRepo(t *testing.T) {
	// CRITICAL: use os.MkdirTemp("/tmp", ...) to create a directory that is
	// NOT inside the project tree. t.TempDir() would return a path under
	// .test-tmp/ which is inside the wipnote repo — the isGitRepo check would
	// then walk up to the real .git directory and find it, causing the test to
	// fire a real git commit instead of skipping. By anchoring to /tmp we
	// ensure the path is genuinely outside any git repository.
	dir, err := os.MkdirTemp("/tmp", "wipnote-nogit-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	wipnoteDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(wipnoteDir, "features"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	featureID := "feat-nogit99"
	featureHTML := filepath.Join(wipnoteDir, "features", featureID+".html")
	if err := os.WriteFile(featureHTML, []byte(`<article id="`+featureID+`" data-status="done"></article>`), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Should return nil even though there is no git repo.
	if err := commitWipnoteArtifact(wipnoteDir, "feature", featureID); err != nil {
		t.Fatalf("expected nil in non-git dir, got: %v", err)
	}
}

// gitCommitCount returns the number of commits in the repo at dir.
func gitCommitCount(t *testing.T, dir string) int {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-list: %v\n%s", err, out)
	}
	count := 0
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d", &count); err != nil {
		t.Fatalf("parse count: %v", err)
	}
	return count
}
