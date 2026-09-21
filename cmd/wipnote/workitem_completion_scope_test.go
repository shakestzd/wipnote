package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// addLinkedWorktree creates a linked worktree of root on a fresh branch,
// outside the main checkout so it never shows up in the owner's git status.
func addLinkedWorktree(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	provGit(t, root, "worktree", "add", "-q", dir, "-b", name)
	return dir
}

func writeSourceFile(t *testing.T, root, rel string) {
	t.Helper()
	abs := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("package foo // "+rel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCanonicalCodeBearingPaths_ScopedToCompletingWorktree pins bug-6c953712
// (GH-#161): with two agents dispatched into sibling worktrees A and B that
// share one .wipnote/, the zero-commit fallback for an item completed from A
// must only see A's own tree, never B's dirty files nor the owner's.
func TestCanonicalCodeBearingPaths_ScopedToCompletingWorktree(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Docs only in A", trackID)
	seedImplementedIn(t, hgDir, id)
	node, err := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if err != nil {
		t.Fatalf("parse artifact: %v", err)
	}

	wtA := addLinkedWorktree(t, tmpDir, "feat-a")
	wtB := addLinkedWorktree(t, tmpDir, "feat-b")
	// Sibling B and the .wipnote owner are both dirty; A is clean.
	writeSourceFile(t, wtB, "public/js/block/editor.js")
	writeSourceFile(t, tmpDir, "internal/owner_dirty.go")

	t.Chdir(wtA)

	if got := canonicalCodeBearingPaths(tmpDir, hgDir, id, node, nil); len(got) != 0 {
		t.Fatalf("clean worktree A must not inherit B's or the owner's dirty files, got %v", got)
	}

	// A's own uncommitted source is A's evidence.
	writeSourceFile(t, wtA, "internal/a_impl.go")
	if got := canonicalCodeBearingPaths(tmpDir, hgDir, id, node, nil); len(got) != 1 || got[0] != "internal/a_impl.go" {
		t.Fatalf("expected only A's dirty file, got %v", got)
	}

	// Committed on A's branch WITHOUT the item id: still A's evidence via the
	// branch diff against the base, and still nothing from B.
	provGit(t, wtA, "add", "--", "internal/a_impl.go")
	provGit(t, wtA, "commit", "-q", "-m", "wip: unlinked commit")
	if got := canonicalCodeBearingPaths(tmpDir, hgDir, id, node, nil); len(got) != 1 || got[0] != "internal/a_impl.go" {
		t.Fatalf("expected A's branch-diff file as evidence, got %v", got)
	}

	// The block message names the scanned tree and branch.
	wiAcceptedAdvisory = ""
	err = runWiSetStatus("feature", id, "done")
	if err == nil {
		t.Fatalf("expected the zero-commit gate to block")
	}
	for _, want := range []string{"Evidence scanned in worktree", "branch feat-a", "internal/a_impl.go"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("block message should contain %q, got: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "editor.js") || strings.Contains(err.Error(), "owner_dirty.go") {
		t.Errorf("block message leaked another tree's files: %v", err)
	}
	item, _ := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if item.Status == models.StatusDone {
		t.Errorf("item must not be marked done when the gate blocks")
	}
}

// TestCheckUncommittedSourceCompleteGate_ScopedToCompletingWorktree pins the
// same scoping for the --allow-dirty gate: tracked files modified in the
// .wipnote owner's checkout do not block a completion run from a clean
// linked worktree.
func TestCheckUncommittedSourceCompleteGate_ScopedToCompletingWorktree(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	wtA := addLinkedWorktree(t, tmpDir, "feat-a")

	// Modify a TRACKED file in the owner's tree (README is committed by
	// provInitRepo) so the owner reads dirty to --untracked-files=no.
	if err := os.WriteFile(filepath.Join(tmpDir, "README"), []byte("owner edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Chdir(wtA)
	if err := checkUncommittedSourceCompleteGate(hgDir, "feat-00000000", false); err != nil {
		t.Fatalf("clean worktree A must not be blocked by the owner's dirty README: %v", err)
	}

	// And A's own tracked edit still blocks, naming the scanned tree.
	if err := os.WriteFile(filepath.Join(wtA, "README"), []byte("a edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := checkUncommittedSourceCompleteGate(hgDir, "feat-00000000", false)
	if err == nil {
		t.Fatalf("expected A's own dirty tracked file to block")
	}
	if !strings.Contains(err.Error(), "scanned worktree") || !strings.Contains(err.Error(), "branch feat-a") {
		t.Errorf("block message should name the scanned worktree and branch, got: %v", err)
	}
}

// TestResolveCompletionWorktree_FallsBackToOwner covers the non-worktree
// paths: CWD in an unrelated repository resolves to the owner and is not
// linked, so no branch diff is attempted.
func TestResolveCompletionWorktree_FallsBackToOwner(t *testing.T) {
	tmpDir, _ := prepProject(t)
	other := t.TempDir()
	provGit(t, other, "init", "-q")
	t.Chdir(other)

	scope := resolveCompletionWorktree(tmpDir)
	gotAbs, _ := filepath.EvalSymlinks(scope.Root)
	wantAbs, _ := filepath.EvalSymlinks(tmpDir)
	if gotAbs != wantAbs {
		t.Errorf("Root = %q, want owner %q", scope.Root, tmpDir)
	}
	if scope.Linked {
		t.Errorf("owner checkout must not be reported as linked: %+v", scope)
	}
	if got := branchTouchedPaths(scope); len(got) != 0 {
		t.Errorf("no branch diff for the owner scope, got %v", got)
	}
}
