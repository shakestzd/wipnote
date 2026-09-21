package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

func committedInTargets(t *testing.T, hgDir, id string) []string {
	t.Helper()
	node, err := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if err != nil {
		t.Fatalf("parse artifact: %v", err)
	}
	var out []string
	for _, e := range node.Edges[string(RelCommittedIn)] {
		out = append(out, e.TargetID)
	}
	return out
}

// TestComplete_WorktreeBareIDCommitAutoLinks pins bug-0816b822 (GH-#162): a
// dispatched agent that commits on its worktree branch with a bare item id in
// the subject completes without --accepted-advisory, and the artifact ends up
// carrying the commit as a committed_in edge without a manual link-commit.
func TestComplete_WorktreeBareIDCommitAutoLinks(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Worktree Impl", trackID)
	seedImplementedIn(t, hgDir, id)

	wtA := addLinkedWorktree(t, tmpDir, "feat-a")
	writeSourceFile(t, wtA, "internal/a_impl.go")
	provGit(t, wtA, "add", "--", "internal/a_impl.go")
	provGit(t, wtA, "commit", "-q", "-m", id+": implement the thing")
	sha := provGit(t, wtA, "rev-parse", "HEAD")

	t.Chdir(wtA)
	wiAcceptedAdvisory = ""
	if err := runWiSetStatus("feature", id, "done"); err != nil {
		t.Fatalf("bare-id worktree commit should satisfy the provenance gate, got: %v", err)
	}
	node, _ := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if node.Status != models.StatusDone {
		t.Fatalf("status = %s, want done", node.Status)
	}
	if got := committedInTargets(t, hgDir, id); len(got) != 1 || got[0] != sha {
		t.Errorf("committed_in edges = %v, want [%s]", got, sha)
	}
}

// TestAutoLinkMessageDerivedCommits_Idempotent: re-running the auto-link
// writes nothing new, and a commit already linked by hand is not duplicated.
func TestAutoLinkMessageDerivedCommits_Idempotent(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Idempotent Link", trackID)
	first := seedProvCommit(t, tmpDir, id, "internal/one.go")
	provGit(t, tmpDir, "commit", "-q", "--allow-empty", "-m", "chore: follow-up "+id)
	second := provGit(t, tmpDir, "rev-parse", "HEAD")
	linkCommitEdge(t, hgDir, id, first, "impl")

	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	var out bytes.Buffer
	if n := autoLinkMessageDerivedCommits(&out, p.Features.Collection, tmpDir, id); n != 1 {
		t.Fatalf("first run linked %d, want 1 (only the unlinked commit)\n%s", n, out.String())
	}
	if !strings.Contains(out.String(), truncate(second, 12)) {
		t.Errorf("expected the newly linked sha in output, got %q", out.String())
	}
	if n := autoLinkMessageDerivedCommits(&out, p.Features.Collection, tmpDir, id); n != 0 {
		t.Fatalf("second run linked %d, want 0", n)
	}
	got := committedInTargets(t, hgDir, id)
	if len(got) != 2 {
		t.Fatalf("committed_in edges = %v, want exactly [%s %s]", got, first, second)
	}
	// Non-git project root: silently nothing.
	if n := autoLinkMessageDerivedCommits(&out, p.Features.Collection, t.TempDir(), id); n != 0 {
		t.Errorf("non-git root must link nothing, got %d", n)
	}
}
