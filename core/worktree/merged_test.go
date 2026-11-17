package worktree

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func addWT(t *testing.T, repo, name string) (path, branch string) {
	t.Helper()
	path = filepath.Join(repo, ".claude", "worktrees", name)
	branch = "feat-" + name
	gitRun(t, repo, "worktree", "add", "-q", "-b", branch, path)
	return
}

func commitFile(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", name)
}

func squashInto(t *testing.T, repo, branch string) {
	t.Helper()
	gitRun(t, repo, "merge", "--squash", branch)
	gitRun(t, repo, "commit", "-q", "-m", "squash "+branch)
}

func find(t *testing.T, entries []Entry, path string) Entry {
	t.Helper()
	for _, e := range entries {
		if e.Path == path {
			return e
		}
	}
	t.Fatalf("no entry for %s in %+v", path, entries)
	return Entry{}
}

func noGH(t *testing.T) {
	t.Helper()
	old := ghMergedPR
	ghMergedPR = func(string, string, string) (int, bool) { return 0, false }
	t.Cleanup(func() { ghMergedPR = old })
}

// wipnote:size-ok: one scenario over a shared repo fixture; splitting would rebuild the repo per case
func TestClean_Matrix(t *testing.T) {
	noGH(t)
	repo := newRepo(t)
	dirty, _ := addWT(t, repo, "aaaaaaa1")
	untracked, _ := addWT(t, repo, "aaaaaaa2")
	unmerged, _ := addWT(t, repo, "aaaaaaa3")
	ancestor, ancBranch := addWT(t, repo, "aaaaaaa4")
	squash, sqBranch := addWT(t, repo, "aaaaaaa5")
	extra, exBranch := addWT(t, repo, "aaaaaaa6")
	locked, _ := addWT(t, repo, "aaaaaaa7")

	if err := os.WriteFile(filepath.Join(dirty, "a.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(untracked, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitFile(t, unmerged, "u.txt")
	commitFile(t, squash, "s1.txt")
	commitFile(t, squash, "s2.txt")
	commitFile(t, extra, "e1.txt")
	squashInto(t, repo, sqBranch)
	squashInto(t, repo, exBranch)
	commitFile(t, extra, "e2.txt")
	gitRun(t, repo, "worktree", "lock", locked)

	// The ancestor branch is merged when main moves past it.
	commitFile(t, repo, "m.txt")

	// Run from the "current" worktree: reuse the dirty-free ancestor? Use a separate one.
	current, curBranch := addWT(t, repo, "aaaaaaa8")

	dry, err := CleanWorktrees(repo, current, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ancestor, squash} {
		if e := find(t, dry, p); !e.Removable {
			t.Errorf("%s should be removable: %+v", p, e)
		}
	}
	if e := find(t, dry, squash); e.Merged != MergedSquash {
		t.Errorf("squash state = %q", e.Merged)
	}
	for _, p := range []string{dirty, untracked, unmerged, extra, locked, current, repo} {
		if e := find(t, dry, p); e.Removable {
			t.Errorf("%s must be kept: %+v", p, e)
		}
	}
	if !find(t, dry, repo).Main || !find(t, dry, current).Current {
		t.Error("main/current marks missing")
	}
	for _, p := range []string{ancestor, squash, dirty} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("dry run removed %s", p)
		}
	}

	if _, err := CleanWorktrees(repo, current, true); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{ancestor, squash} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("%s should be removed", p)
		}
	}
	for _, p := range []string{dirty, untracked, unmerged, extra, locked, current, repo} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should remain", p)
		}
	}
	branches := gitRun(t, repo, "branch", "--format=%(refname:short)")
	for _, b := range []string{ancBranch, sqBranch} {
		if strings.Contains(branches, b) {
			t.Errorf("branch %s should be deleted", b)
		}
	}
	for _, b := range []string{exBranch, curBranch, "main"} {
		if !strings.Contains(branches, b) {
			t.Errorf("branch %s should remain", b)
		}
	}
}

func TestMergedState_GhRule(t *testing.T) {
	repo := newRepo(t)
	_, branch := addWT(t, repo, "bbbbbbb1")
	tip := gitRun(t, repo, "rev-parse", branch)
	commitFile(t, filepath.Join(repo, ".claude", "worktrees", "bbbbbbb1"), "p.txt")
	tip = gitRun(t, repo, "rev-parse", branch)
	commitFile(t, repo, "other.txt") // main diverges so rules a and b fail

	old := ghMergedPR
	t.Cleanup(func() { ghMergedPR = old })
	ghMergedPR = func(_, b, sha string) (int, bool) { return 42, b == branch && sha == tip }
	if got := MergedState(repo, "main", branch, tip); got != "merged (PR #42)" {
		t.Errorf("got %q", got)
	}
	ghMergedPR = func(string, string, string) (int, bool) { return 0, false }
	if got := MergedState(repo, "main", branch, tip); got != MergedNot {
		t.Errorf("got %q", got)
	}
}

func TestList_DetachedAndWorkItem(t *testing.T) {
	noGH(t)
	repo := newRepo(t)
	path := filepath.Join(repo, ".claude", "worktrees", "det")
	gitRun(t, repo, "worktree", "add", "-q", "--detach", path)
	wt, _ := addWT(t, repo, "cccccccc")
	entries, err := ListWorktrees(repo, repo)
	if err != nil {
		t.Fatal(err)
	}
	d := find(t, entries, path)
	if d.Merged != MergedNoBranch || d.Removable {
		t.Errorf("detached: %+v", d)
	}
	if e := find(t, entries, wt); e.WorkItem != "feat-cccccccc" || e.RelPath != filepath.Join(".claude", "worktrees", "cccccccc") {
		t.Errorf("entry: %+v", e)
	}
}
