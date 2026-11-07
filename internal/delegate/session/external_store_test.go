package session

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func extGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func extRepo(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	extGit(t, repo, "init", "-q")
	extGit(t, repo, "commit", "-q", "--allow-empty", "-m", "first")
	return repo
}

// The run store stays with the store owner while the worker's checkout goes in
// the source repo, which stays clean, and the checkout's hooks find the store.
func TestWorktree_ExternalStoreSplitsTheRoots(t *testing.T) {
	repo := extRepo(t)
	shadow, _, err := paths.CreateExternalStore(repo, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	store, err := runstore.New(shadow)
	if err != nil {
		t.Fatal(err)
	}
	o := SpawnOptions{Name: "w1", Root: shadow, Worktree: true, Harness: HarnessClaude}
	if err := createWorktree(Deps{Store: store}, &o); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(repo, ".claude", "worktrees", "w1")
	if o.wt.Path != want || o.Cwd != want || o.WorkDir != want {
		t.Fatalf("checkout at %+v, want %s", o.wt, want)
	}
	if o.ProjectDir != shadow {
		t.Fatalf("worker's --project-dir = %s, want the store owner %s", o.ProjectDir, shadow)
	}
	if !strings.HasPrefix(store.Root(), shadow) {
		t.Fatalf("run store at %s", store.Root())
	}
	if got := paths.ResolveViaGitCommonDir(want); got != shadow {
		t.Fatalf("hooks in the checkout resolve to %q", got)
	}
	if out := extGit(t, repo, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Fatalf("code repo is dirty:\n%s", out)
	}
	if out := extGit(t, shadow, "worktree", "list"); strings.Contains(out, "w1") {
		t.Fatalf("checkout made in the store's repo:\n%s", out)
	}

	var res StopResult
	settleWorktree(&res, Info{ProjectDir: shadow, Worktree: want, Branch: o.wt.Branch, BaseSHA: o.wt.BaseSHA})
	if !res.WorktreeRemoved {
		t.Fatalf("an untouched checkout was not removed: %+v", res)
	}
	if _, err := os.Stat(want); err == nil {
		t.Fatal("checkout still on disk")
	}
}

func TestWorktree_InRepoStoreUnchanged(t *testing.T) {
	repo := extRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	store, err := runstore.New(repo)
	if err != nil {
		t.Fatal(err)
	}
	o := SpawnOptions{Name: "w1", Root: repo, Worktree: true, Harness: HarnessClaude}
	if err := createWorktree(Deps{Store: store}, &o); err != nil {
		t.Fatal(err)
	}
	if o.wt.Path != filepath.Join(repo, ".claude", "worktrees", "w1") || o.ProjectDir != repo {
		t.Fatalf("got %+v, project dir %s", o.wt, o.ProjectDir)
	}
	data, _ := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if strings.Contains(string(data), ".claude/worktrees") {
		t.Fatalf("an in-repo store's repo got an exclude: %q", data)
	}
}

// workersKey is the sha of <project>/.wipnote/delegate for an in-repo store;
// changing it would orphan every live project's tab.
func TestWorkersKey_StableForInRepoStore(t *testing.T) {
	root := "/some/project"
	store, err := runstore.New(root)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(filepath.Join(root, ".wipnote", "delegate")))
	if got, want := workersKey(store), hex.EncodeToString(sum[:6]); got != want {
		t.Fatalf("workersKey = %s, want %s", got, want)
	}
}

// A worker spawned from the code root runs there, on the store's project.
func TestLinkedDirs_ExternalStoreWorkerRunsInCodeRoot(t *testing.T) {
	repo := extRepo(t)
	shadow, _, err := paths.CreateExternalStore(repo, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	o := SpawnOptions{Root: shadow, Cwd: repo, Harness: HarnessClaude}
	linkedDirs(&o)
	if o.WorkDir != repo || o.ProjectDir != shadow {
		t.Fatalf("WorkDir %s, ProjectDir %s", o.WorkDir, o.ProjectDir)
	}
}
