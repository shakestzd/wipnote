package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// TestStartAdvisory_SilentWhenNothingLinked: a fresh item in a clean repo
// prints nothing, and a non-git root prints nothing.
func TestStartAdvisory_SilentWhenNothingLinked(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Fresh Bug", trackID)

	var out bytes.Buffer
	emitAlreadyFixedAdvisory(&out, tmpDir, id, nil)
	if out.Len() != 0 {
		t.Errorf("expected silence for an untouched item, got %q", out.String())
	}
	emitAlreadyFixedAdvisory(&out, t.TempDir(), id, nil)
	if out.Len() != 0 {
		t.Errorf("expected silence for a non-git root, got %q", out.String())
	}
}

// TestStartAdvisory_LinkedCommitsSignal names commits linked by message and
// by committed_in edge, capped at five.
func TestStartAdvisory_LinkedCommitsSignal(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Already Fixed", trackID)
	sha := seedProvCommit(t, tmpDir, id, "internal/fixed.go")

	var out bytes.Buffer
	emitAlreadyFixedAdvisory(&out, tmpDir, id, nil)
	got := out.String()
	for _, want := range []string{"already has 1 linked commit", truncate(sha, 12), "impl (" + id + ")"} {
		if !strings.Contains(got, want) {
			t.Errorf("advisory should contain %q, got %q", want, got)
		}
	}

	// Six more via edges: only five are listed, the rest are counted.
	// The seeded commit's own file also cites the id, so the grep signal is
	// expected here too.
	node, _ := htmlparse.ParseFile(filepath.Join(hgDir, "bugs", id+".html"))
	if node.Edges == nil {
		node.Edges = map[string][]models.Edge{}
	}
	for i := 0; i < 6; i++ {
		provGit(t, tmpDir, "commit", "-q", "--allow-empty", "-m", "unrelated subject")
		node.Edges[string(RelCommittedIn)] = append(node.Edges[string(RelCommittedIn)], models.Edge{
			TargetID: provGit(t, tmpDir, "rev-parse", "HEAD"), Relationship: RelCommittedIn,
		})
	}
	out.Reset()
	emitAlreadyFixedAdvisory(&out, tmpDir, id, node)
	got = out.String()
	if !strings.Contains(got, "already has 7 linked commit") || !strings.Contains(got, "… and 2 more") {
		t.Errorf("expected 7 commits with a 5-line cap, got %q", got)
	}
	if !strings.Contains(got, "internal/fixed.go") {
		t.Errorf("expected the citing source file in the grep signal, got %q", got)
	}
}

// TestStartAdvisory_SourceMentionSignal reports tracked and untracked files
// that cite the id, never the item's own artifact under .wipnote/.
func TestStartAdvisory_SourceMentionSignal(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Cited In Source", trackID)

	// Untracked file citing the id in a comment.
	if err := os.MkdirAll(filepath.Join(tmpDir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "scripts", "validate.sh"),
		[]byte("# guard added for ("+id+")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	emitAlreadyFixedAdvisory(&out, tmpDir, id, nil)
	got := out.String()
	if !strings.Contains(got, "cited in 1 source file") || !strings.Contains(got, "scripts/validate.sh") {
		t.Errorf("expected the untracked citing file, got %q", got)
	}
	if strings.Contains(got, "  .wipnote/") || strings.Contains(got, "linked commit") {
		t.Errorf("artifact must not be listed and no commit signal expected, got %q", got)
	}
	if strings.Contains(got, "cited in 2 source file") {
		t.Errorf("the item's own artifact must not be counted, got %q", got)
	}

	// Committed WITHOUT the id in the message: still only the grep signal.
	provGit(t, tmpDir, "add", "--", "scripts/validate.sh")
	provGit(t, tmpDir, "commit", "-q", "-m", "chore: validator guard")
	out.Reset()
	emitAlreadyFixedAdvisory(&out, tmpDir, id, nil)
	if got := out.String(); !strings.Contains(got, "scripts/validate.sh") || strings.Contains(got, "linked commit") {
		t.Errorf("expected only the grep signal for a tracked citation, got %q", got)
	}
}

// TestStart_EmitsAlreadyFixedAdvisory: the real start path prints the
// advisory to stderr and still succeeds.
func TestStart_EmitsAlreadyFixedAdvisory(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Start Advisory", trackID)
	seedProvCommit(t, tmpDir, id, "internal/fixed.go")

	var startErr error
	stderr := captureStderr(t, func() {
		startErr = runWiSetStatus("bug", id, "in-progress")
	})
	if startErr != nil {
		t.Fatalf("start must not be blocked by the advisory: %v", startErr)
	}
	if !strings.Contains(stderr, "start advisory: "+id+" already has 1 linked commit") {
		t.Errorf("expected the advisory on stderr, got %q", stderr)
	}
	node, _ := htmlparse.ParseFile(filepath.Join(hgDir, "bugs", id+".html"))
	if node.Status != models.StatusInProgress {
		t.Errorf("status = %s, want in-progress", node.Status)
	}
}
