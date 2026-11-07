package worktree_test

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/worktree"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func externalRepo(t *testing.T) (repo, store string) {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	repo = filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, repo, "init", "-q")
	gitT(t, repo, "commit", "-q", "--allow-empty", "-m", "first")
	store, _, err = paths.CreateExternalStore(repo, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	return repo, store
}

func TestCreateOnNewBranch_ExternalStoreLeavesCodeRepoClean(t *testing.T) {
	repo, store := externalRepo(t)
	path := filepath.Join(repo, ".claude", "worktrees", "w1")
	if _, err := worktree.CreateOnNewBranch(repo, path, "delegate/w1", "HEAD", io.Discard); err != nil {
		t.Fatal(err)
	}
	if out := gitT(t, repo, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Fatalf("code repo is dirty:\n%s", out)
	}
	if got := paths.ResolveViaGitCommonDir(path); got != store {
		t.Fatalf("hooks in the worktree resolve to %q, want the store %q", got, store)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(data), "/.claude/worktrees/") != 1 {
		t.Fatalf("exclude file: %q", data)
	}
	worktree.ExcludeWorktreesDir(repo)
	data, _ = os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Count(string(data), "/.claude/worktrees/") != 1 {
		t.Fatalf("exclude added twice: %q", data)
	}
}

func TestExcludeWorktreesDir_InRepoStoreAndIgnoredRepoUntouched(t *testing.T) {
	repo, _ := externalRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(".claude/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	worktree.ExcludeWorktreesDir(repo)
	after, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if string(before) != string(after) {
		t.Fatalf("an already ignored folder was excluded: %q", after)
	}

	if err := os.MkdirAll(filepath.Join(repo, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	worktree.ExcludeWorktreesDir(repo)
	after, _ = os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if string(before) != string(after) {
		t.Fatalf("an in-repo store's repo was changed: %q", after)
	}
}

func TestEnsureWipnoteGitignore_DoesNotCreateStoreInRepo(t *testing.T) {
	base := t.TempDir()
	t.Setenv("HOME", filepath.Join(base, "home"))
	gitT(t, base, "init", "-q")
	worktree.EnsureWipnoteGitignore(base)
	if _, err := os.Stat(filepath.Join(base, ".wipnote")); err == nil {
		t.Fatal(".wipnote created in a repo with no store")
	}
}

func TestEnsureForFeature_ExternalStoreUsesCodeRepo(t *testing.T) {
	repo, store := externalRepo(t)
	restore := worktree.SetReindexFnForTest(func(string, io.Writer) {})
	defer worktree.SetReindexFnForTest(restore)
	// The launcher hands either root over.
	path, created, err := worktree.EnsureForFeatureStatus("feat-11111111", store, io.Discard)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if want := filepath.Join(repo, ".claude", "worktrees", "feat-11111111"); path != want {
		t.Fatalf("worktree at %s, want %s", path, want)
	}
	if out := gitT(t, repo, "worktree", "list"); !strings.Contains(out, "feat-11111111") {
		t.Fatalf("worktree not in the code repo:\n%s", out)
	}
	if out := gitT(t, store, "worktree", "list"); strings.Contains(out, "feat-11111111") {
		t.Fatalf("worktree made in the store's repo:\n%s", out)
	}
	if out := gitT(t, repo, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Fatalf("code repo is dirty:\n%s", out)
	}
}
