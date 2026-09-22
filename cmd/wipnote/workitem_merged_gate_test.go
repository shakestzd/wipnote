package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

// seedOriginRemote gives the fixture repo a bare `origin` holding its current
// branch, so refs/remotes/origin/<branch> (and origin/HEAD) resolve as the
// upstream default. Returns the branch name that was pushed.
func seedOriginRemote(t *testing.T, root string) string {
	t.Helper()
	bare := filepath.Join(t.TempDir(), "origin.git")
	provGit(t, root, "init", "-q", "--bare", bare)
	branch := provGit(t, root, "rev-parse", "--abbrev-ref", "HEAD")
	provGit(t, root, "remote", "add", "origin", bare)
	provGit(t, root, "push", "-q", "origin", branch)
	provGit(t, root, "remote", "set-head", "origin", branch)
	return branch
}

// openFeatureCollection returns the project and its feature collection for
// calling a gate directly.
func openFeatureCollection(t *testing.T, hgDir string) (*workitem.Project, *workitem.Collection) {
	t.Helper()
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatalf("open project: %v", err)
	}
	t.Cleanup(func() { p.Close() })
	return p, p.Features.Collection
}

// commitOnBranch commits a linked source change on a fresh linked worktree
// branch that is never merged, and returns the sha.
func commitOnUnmergedBranch(t *testing.T, root, id string) string {
	t.Helper()
	wt := addLinkedWorktree(t, root, "feat-orphan")
	writeSourceFile(t, wt, "internal/orphan.go")
	provGit(t, wt, "add", "--", "internal/orphan.go")
	provGit(t, wt, "commit", "-q", "-m", "impl ("+id+")")
	return provGit(t, wt, "rev-parse", "HEAD")
}

func TestMergedUpstreamGate_UnmergedBranchBlocks(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	branch := seedOriginRemote(t, tmpDir)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Orphan Work", trackID)
	seedImplementedIn(t, hgDir, id)
	commitOnUnmergedBranch(t, tmpDir, id)

	p, col := openFeatureCollection(t, hgDir)
	err := checkMergedUpstreamCompleteGate(p, col, "feature", id, "")
	if err == nil {
		t.Fatalf("a linked commit only on an unmerged branch must block")
	}
	for _, want := range []string{"origin/" + branch, "pull request", "--allow-orphan"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("block message should contain %q, got: %v", want, err)
		}
	}

	// End to end: the provenance gate passes (commit exists) but completion
	// is still refused and the item is not done.
	wiAcceptedAdvisory, wiAllowOrphan = "", ""
	if err := runWiSetStatus("feature", id, "done"); err == nil || !strings.Contains(err.Error(), "merged upstream") {
		t.Fatalf("expected the merged-upstream gate to refuse completion, got: %v", err)
	}
	node, _ := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if node.Status == models.StatusDone {
		t.Errorf("item must not be done when the gate blocks")
	}
}

func TestMergedUpstreamGate_MergedCommitPasses(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	branch := seedOriginRemote(t, tmpDir)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Merged Work", trackID)
	seedImplementedIn(t, hgDir, id)
	seedProvCommit(t, tmpDir, id, "internal/merged.go")
	provGit(t, tmpDir, "push", "-q", "origin", branch)

	p, col := openFeatureCollection(t, hgDir)
	if err := checkMergedUpstreamCompleteGate(p, col, "feature", id, ""); err != nil {
		t.Fatalf("a linked commit reachable from origin/%s must pass, got: %v", branch, err)
	}
	// An unmerged commit alongside the merged one changes nothing: one
	// reachable commit is enough.
	commitOnUnmergedBranch(t, tmpDir, id)
	if err := checkMergedUpstreamCompleteGate(p, col, "feature", id, ""); err != nil {
		t.Fatalf("one merged commit must satisfy the gate, got: %v", err)
	}
}

func TestMergedUpstreamGate_AllowOrphanRecordsNote(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	seedOriginRemote(t, tmpDir)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Branch Local", trackID)
	seedImplementedIn(t, hgDir, id)
	commitOnUnmergedBranch(t, tmpDir, id)

	wiAcceptedAdvisory = ""
	wiAllowOrphan = "branch-local by design"
	t.Cleanup(func() { wiAllowOrphan = "" })
	if err := runWiSetStatus("feature", id, "done"); err != nil {
		t.Fatalf("--allow-orphan should complete the item, got: %v", err)
	}
	node, _ := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if node.Status != models.StatusDone {
		t.Errorf("status = %s, want done", node.Status)
	}
	if got := allowOrphanOf(node); got != "branch-local by design" {
		t.Errorf("allow-orphan rationale not persisted, got %q", got)
	}
	if acceptedAdvisoryOf(node) != "" {
		t.Errorf("allow-orphan must not masquerade as an accepted-advisory note")
	}
}

func TestMergedUpstreamGate_SkipsWithoutRemoteOrCommits(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "No Remote", trackID)
	seedImplementedIn(t, hgDir, id)
	p, col := openFeatureCollection(t, hgDir)

	// No linked commits at all: the provenance gate owns that case.
	if err := checkMergedUpstreamCompleteGate(p, col, "feature", id, ""); err != nil {
		t.Fatalf("zero linked commits must skip, got: %v", err)
	}
	// Unmerged commit but no remote: nothing to be merged into, skip.
	commitOnUnmergedBranch(t, tmpDir, id)
	if err := checkMergedUpstreamCompleteGate(p, col, "feature", id, ""); err != nil {
		t.Fatalf("no remote must skip, got: %v", err)
	}
	// Non-git project root: skip.
	nonGit, err := workitem.Open(filepath.Join(t.TempDir(), ".wipnote"), "claude-code")
	if err != nil {
		t.Fatalf("open non-git project: %v", err)
	}
	defer nonGit.Close()
	if err := checkMergedUpstreamCompleteGate(nonGit, col, "feature", id, ""); err != nil {
		t.Fatalf("non-git project must skip, got: %v", err)
	}
}
