package worktree

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	gitRun(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "init")
	return dir
}

func TestCreateOnNewBranch_CreatesAtBase(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, ".claude", "worktrees", "w1")
	sha, err := CreateOnNewBranch(repo, path, "delegate/w1", "HEAD", io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want := gitRun(t, repo, "rev-parse", "HEAD"); sha != want {
		t.Fatalf("base %s want %s", sha, want)
	}
	if got := gitRun(t, path, "rev-parse", "--abbrev-ref", "HEAD"); got != "delegate/w1" {
		t.Fatalf("branch %s", got)
	}
}

func TestCreateOnNewBranch_RefusesTakenBranchOrPath(t *testing.T) {
	repo := newRepo(t)
	path := filepath.Join(repo, ".claude", "worktrees", "w1")
	if _, err := CreateOnNewBranch(repo, path, "delegate/w1", "HEAD", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateOnNewBranch(repo, path, "delegate/w1", "HEAD", io.Discard); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("taken path: %v", err)
	}
	other := filepath.Join(repo, ".claude", "worktrees", "w2")
	if _, err := CreateOnNewBranch(repo, other, "delegate/w1", "HEAD", io.Discard); err == nil || !strings.Contains(err.Error(), "branch delegate/w1 already exists") {
		t.Fatalf("taken branch: %v", err)
	}
	if _, err := os.Stat(other); err == nil {
		t.Fatal("a refused create left a folder")
	}
	if _, err := CreateOnNewBranch(repo, other, "delegate/w2", "no-such-ref", io.Discard); err == nil {
		t.Fatal("bad base accepted")
	}
	if _, err := os.Stat(other); err == nil {
		t.Fatal("a failed create left a folder")
	}
}

func TestRemoveIfClean(t *testing.T) {
	repo := newRepo(t)
	mk := func(name string) (string, string) {
		p := filepath.Join(repo, ".claude", "worktrees", name)
		sha, err := CreateOnNewBranch(repo, p, "delegate/"+name, "HEAD", io.Discard)
		if err != nil {
			t.Fatal(err)
		}
		return p, sha
	}
	p, sha := mk("clean")
	if kept, why := RemoveIfClean(repo, p, "delegate/clean", sha); kept {
		t.Fatalf("clean worktree kept: %s", why)
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("folder remains")
	}
	if exec.Command("git", "-C", repo, "rev-parse", "--verify", "refs/heads/delegate/clean").Run() == nil {
		t.Fatal("branch remains")
	}

	p, sha = mk("dirty")
	_ = os.WriteFile(filepath.Join(p, "a.txt"), []byte("changed\n"), 0o644)
	if kept, why := RemoveIfClean(repo, p, "delegate/dirty", sha); !kept || !strings.Contains(why, "changes") {
		t.Fatalf("dirty: kept=%v why=%q", kept, why)
	}
	p, sha = mk("untracked")
	_ = os.WriteFile(filepath.Join(p, "new.txt"), []byte("x"), 0o644)
	if kept, _ := RemoveIfClean(repo, p, "delegate/untracked", sha); !kept {
		t.Fatal("untracked file removed")
	}
	p, sha = mk("committed")
	_ = os.WriteFile(filepath.Join(p, "b.txt"), []byte("b"), 0o644)
	gitRun(t, p, "add", ".")
	gitRun(t, p, "commit", "-q", "-m", "work")
	if kept, why := RemoveIfClean(repo, p, "delegate/committed", sha); !kept || !strings.Contains(why, "commit") {
		t.Fatalf("committed: kept=%v why=%q", kept, why)
	}
	for _, n := range []string{"dirty", "untracked", "committed"} {
		if _, err := os.Stat(filepath.Join(repo, ".claude", "worktrees", n)); err != nil {
			t.Fatalf("%s was removed: %v", n, err)
		}
	}
}

func TestRemoveIfClean_GoneIsNotKept(t *testing.T) {
	repo := newRepo(t)
	if kept, _ := RemoveIfClean(repo, filepath.Join(repo, "nope"), "delegate/nope", "abc"); kept {
		t.Fatal("a missing worktree reported as kept")
	}
}

// Commits on a detached HEAD leave the branch at its base and the checkout
// clean, so only HEAD shows them. Removing the worktree would lose them.
func TestRemoveIfClean_KeepsCommitsOnDetachedHead(t *testing.T) {
	repo := newRepo(t)
	base := gitRun(t, repo, "rev-parse", "HEAD")
	path := filepath.Join(t.TempDir(), "wt")
	gitRun(t, repo, "worktree", "add", "-q", "-b", "delegate/d", path, base)
	gitRun(t, path, "checkout", "-q", "--detach")
	gitRun(t, path, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "--allow-empty", "-m", "only here")

	kept, why := RemoveIfClean(repo, path, "delegate/d", base)
	if !kept || !strings.Contains(why, "HEAD") {
		t.Fatalf("kept = %v, why = %q; want the worktree kept", kept, why)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("worktree was removed: %v", err)
	}
}
