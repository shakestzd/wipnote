package paths

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

// TestGitTopLevel_FromSubdir pins that the helper resolves the given dir,
// not the process cwd, and errors outside a repository or with no dir.
func TestGitTopLevel_FromSubdir(t *testing.T) {
	repo := initGitRepo(t)
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir()) // cwd outside the repo must not matter
	got, err := GitTopLevel(sub)
	if err != nil || got != repo {
		t.Fatalf("GitTopLevel(sub) = %q, %v; want %q", got, err, repo)
	}
	if _, err := GitTopLevel(t.TempDir()); err == nil {
		t.Error("GitTopLevel(non-repo) returned no error")
	}
	if _, err := GitTopLevel(""); err == nil {
		t.Error("GitTopLevel(\"\") returned no error; it must not use the cwd")
	}
}

// TestGitHead_RunsInDir pins that GitHead reads HEAD of the given dir and
// returns "" for a non-repo, an empty repo, or no dir.
func TestGitHead_RunsInDir(t *testing.T) {
	repo := initGitRepo(t)
	t.Chdir(t.TempDir())
	want, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := GitHead(repo); got != strings.TrimSpace(string(want)) || len(got) != 40 {
		t.Fatalf("GitHead(repo) = %q, want %q", got, want)
	}
	empty := t.TempDir()
	if out, err := exec.Command("git", "-C", empty, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	for _, dir := range []string{t.TempDir(), empty, ""} {
		if got := GitHead(dir); got != "" {
			t.Errorf("GitHead(%q) = %q, want empty", dir, got)
		}
	}
}
