package hooks

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// reconcileGit runs a git command in root and returns trimmed stdout.
func reconcileGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

func reconcileCommitCount(t *testing.T, root string) int {
	t.Helper()
	n, err := strconv.Atoi(reconcileGit(t, root, "rev-list", "--count", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// TestReconcile_OneCommitPerPass pins that a pass with several dirty done
// artifacts makes exactly one commit, listing every id in the body.
func TestReconcile_OneCommitPerPass(t *testing.T) {
	clearNestedEnv(t)
	root := t.TempDir()
	gitInitRepo(t, root)
	before := reconcileCommitCount(t, root)

	ids := []string{"feat-aaa", "feat-bbb", "bug-ccc"}
	writeArtifact(t, root, "feature", "feat-aaa")
	writeArtifact(t, root, "feature", "feat-bbb")
	writeArtifact(t, root, "bug", "bug-ccc")

	got := reconcileDoneButUncommittedCanonical(root)
	if len(got) != 3 {
		t.Fatalf("want 3 ids committed, got %v", got)
	}
	if after := reconcileCommitCount(t, root); after != before+1 {
		t.Fatalf("want exactly one new commit, before=%d after=%d", before, after)
	}
	msg := reconcileGit(t, root, "log", "-1", "--format=%B")
	if !strings.HasPrefix(msg, "wipnote: reconcile 3 items") {
		t.Fatalf("unexpected subject: %q", msg)
	}
	for _, id := range ids {
		if !strings.Contains(msg, id) {
			t.Errorf("body missing %s: %q", id, msg)
		}
	}
	if again := reconcileDoneButUncommittedCanonical(root); len(again) != 0 {
		t.Fatalf("second pass must be a no-op, got %v", again)
	}
}

// TestReconcile_SingleItemKeepsIDSubject keeps the per-item subject for a
// one-item pass.
func TestReconcile_SingleItemKeepsIDSubject(t *testing.T) {
	clearNestedEnv(t)
	root := t.TempDir()
	gitInitRepo(t, root)
	writeArtifact(t, root, "feature", "feat-solo")

	if got := reconcileDoneButUncommittedCanonical(root); len(got) != 1 || got[0] != "feat-solo" {
		t.Fatalf("got %v", got)
	}
	if subj := reconcileGit(t, root, "log", "-1", "--format=%s"); subj != "wipnote: reconcile feat-solo" {
		t.Fatalf("unexpected subject: %q", subj)
	}
}
