package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

// evidenceFor builds fresh completion evidence for calling one gate directly.
func evidenceFor(p *workitem.Project, col *workitem.Collection, id string) *completionEvidence {
	node, _ := col.Get(id)
	return newCompletionEvidence(p.ProjectDir, id, node)
}

// linkedCommitsAt resolves the item's linked commits fresh, as the completion
// gates would, for calling the auto-link directly.
func linkedCommitsAt(col *workitem.Collection, repoRoot, id string) []string {
	node, _ := col.Get(id)
	return canonicalLinkedCommits(repoRoot, id, node)
}

// countLinkedCommits wraps the linkedCommitsFn seam and returns a pointer to
// the number of calls made through it.
func countLinkedCommits(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := linkedCommitsFn
	linkedCommitsFn = func(repoRoot, id string, node *models.Node) []string {
		calls++
		return orig(repoRoot, id, node)
	}
	t.Cleanup(func() { linkedCommitsFn = orig })
	return &calls
}

// TestFeatureComplete_LinkedCommitsComputedOnce pins feat-c30d0792: one
// `feature complete` walks `git log --all --grep` once, however many gates
// need the linked commits. The fixture exercises every consumer — a dirty
// unrelated file (dirty gate), a linked commit (provenance), an origin remote
// (merged-upstream), the dependency-research gate and the auto-link — and the
// completion must still see the commit: done, with the committed_in edge.
func TestFeatureComplete_LinkedCommitsComputedOnce(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Linked Once", trackID)
	seedImplementedIn(t, hgDir, id)
	sha := seedProvCommit(t, tmpDir, id, "internal/linked.go")
	writeSourceFile(t, tmpDir, "internal/unrelated.go")
	provGit(t, tmpDir, "add", "--", "internal/unrelated.go")
	provGit(t, tmpDir, "commit", "-q", "-m", "chore: unrelated file")
	seedOriginRemote(t, tmpDir)
	// Dirty an unrelated tracked file so the uncommitted-source gate looks
	// up the item's commits too.
	if err := os.WriteFile(filepath.Join(tmpDir, "internal/unrelated.go"), []byte("package foo // dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	calls := countLinkedCommits(t)
	t.Chdir(tmpDir)
	wiAcceptedAdvisory, wiAllowOrphan = "", ""
	if err := runWiSetStatus("feature", id, "done"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if *calls != 1 {
		t.Errorf("linked commits computed %d times in one completion, want 1", *calls)
	}
	node, err := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != models.StatusDone {
		t.Errorf("status = %s, want done", node.Status)
	}
	if got := committedInTargets(t, hgDir, id); len(got) != 1 || got[0] != sha {
		t.Errorf("committed_in edges = %v, want [%s] (gates must still see the commit)", got, sha)
	}
}
