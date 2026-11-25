package paths_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

// externalEnv points HOME at scratch space so no test can reach the real
// ~/.wipnote, and returns a real-path scratch folder.
func externalEnv(t *testing.T) string {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_SESSION_ID", "")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := os.MkdirAll(filepath.Join(base, "home"), 0o755); err != nil {
		t.Fatal(err)
	}
	return base
}

func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir,
		"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newRepo(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	mustGit(t, dir, "init", "-q")
	mustGit(t, dir, "commit", "-q", "--allow-empty", "-m", "first")
	return dir
}

func mkStore(t *testing.T, owner string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(owner, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func resolveAt(t *testing.T, dir string) string {
	t.Helper()
	got, err := paths.ResolveProjectDir(paths.ProjectDirOptions{EventCWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func createStore(t *testing.T, dir string) string {
	t.Helper()
	root, created, err := paths.CreateExternalStore(dir, paths.OriginInit, nil)
	if err != nil || !created {
		t.Fatalf("CreateExternalStore: created=%v err=%v", created, err)
	}
	return root
}

func TestResolve_InRepoStoreWinsOverShadow(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	createStore(t, repo)
	mkStore(t, repo)
	if got := resolveAt(t, repo); got != repo {
		t.Fatalf("got %s, want the in-repo store at %s", got, repo)
	}
	if got := paths.ExternalStoreFor(repo); got != "" {
		t.Fatalf("ExternalStoreFor = %s with an in-repo store", got)
	}
}

func TestResolve_ParentStoreAboveToplevelIgnored(t *testing.T) {
	base := externalEnv(t)
	mkStore(t, base)
	repo := newRepo(t, filepath.Join(base, "nested", "repo"))
	sub := filepath.Join(repo, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveAt(t, sub); got != sub {
		t.Fatalf("no store: got %s, want the start dir and never the parent %s", got, base)
	}
	if got := paths.StoreOwnerFor(sub); got != "" {
		t.Fatalf("StoreOwnerFor = %s in a repo with no store", got)
	}
	root := createStore(t, repo)
	if got := resolveAt(t, sub); got != root {
		t.Fatalf("got %s, want the shadow root %s", got, root)
	}
	if got := paths.StoreOwnerFor(sub); got != root {
		t.Fatalf("StoreOwnerFor = %s once a shadow root exists", got)
	}
}

func TestResolve_NestedInRepoStoreStillFoundBelowToplevel(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	sub := filepath.Join(repo, "pkg", "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	mkStore(t, filepath.Join(repo, "pkg"))
	if got := resolveAt(t, sub); got != filepath.Join(repo, "pkg") {
		t.Fatalf("got %s", got)
	}
}

func TestResolve_LinkedWorktreeSharesShadow(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "repo"))
	root := createStore(t, repo)
	wt := filepath.Join(base, "wt")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "w", wt)
	if got := resolveAt(t, wt); got != root {
		t.Fatalf("worktree resolves to %s, want %s", got, root)
	}
	if got := paths.ResolveViaGitCommonDir(wt); got != root {
		t.Fatalf("ResolveViaGitCommonDir = %s, want %s", got, root)
	}
	if got := paths.ShadowRootFor(paths.MainRepoRoot(wt)); got != root {
		t.Fatalf("identity differs in a worktree: %s vs %s", got, root)
	}
	if got := paths.CodeRoot(root); got != repo {
		t.Fatalf("CodeRoot = %s, want the main repo %s", got, repo)
	}
}

func TestResolve_LinkedWorktreeOfInRepoStoreUnchanged(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "repo"))
	mkStore(t, repo)
	wt := filepath.Join(base, "wt")
	mustGit(t, repo, "worktree", "add", "-q", "-b", "w", wt)
	if got := resolveAt(t, wt); filepath.Clean(got) != repo {
		t.Fatalf("got %s, want %s", got, repo)
	}
}

func TestResolve_HomeToplevelIsNotARepo(t *testing.T) {
	base := externalEnv(t)
	home := newRepo(t, filepath.Join(base, "home"))
	dev := filepath.Join(home, "Dev")
	mkStore(t, dev)
	sub := filepath.Join(dev, "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveAt(t, sub); got != dev {
		t.Fatalf("got %s, want the nearest store %s by walk-up", got, dev)
	}
	if got := paths.ResolveViaGitCommonDir(sub); got != "" {
		t.Fatalf("ResolveViaGitCommonDir = %s under a $HOME toplevel", got)
	}
	// A plain folder under it gets a store keyed by the folder, not one for $HOME.
	plain := filepath.Join(home, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	root, _, err := paths.CreateExternalStore(plain, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths.CodeRoot(root); got != plain {
		t.Fatalf("store keyed by %s, want %s", got, plain)
	}
}

// ~/.wipnote holds store-shaped folders and must never resolve as a project.
func TestHome_NeverAProjectStore(t *testing.T) {
	base := externalEnv(t)
	home := filepath.Join(base, "home")
	mkStore(t, home)
	sub := filepath.Join(home, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{"home": home, "below": sub} {
		if got := paths.StoreOwnerFor(dir); got != "" {
			t.Errorf("%s: StoreOwnerFor = %s", name, got)
		}
		if got := resolveAt(t, dir); got == home && dir != home {
			t.Errorf("%s: resolved to $HOME by walk-up", name)
		}
	}
	if paths.IsStoreOwner(home) {
		t.Error("IsStoreOwner($HOME) = true")
	}
	if _, err := paths.ResolveProjectDir(paths.ProjectDirOptions{ExplicitDir: home}); err == nil {
		t.Error("--project-dir $HOME accepted")
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", home)
	if got := resolveAt(t, sub); got == home {
		t.Error("WIPNOTE_PROJECT_DIR=$HOME accepted")
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	if paths.StoreCreationAllowed(home) {
		t.Error("StoreCreationAllowed($HOME) = true")
	}
}

// A linked worktree of a dotfiles repo must not reach ~/.wipnote through the
// main root, which is the case the toplevel check alone misses.
func TestHome_NotReachedThroughLinkedWorktree(t *testing.T) {
	base := externalEnv(t)
	home := newRepo(t, filepath.Join(base, "home"))
	mkStore(t, home)
	wt := filepath.Join(base, "wt")
	mustGit(t, home, "worktree", "add", "-q", "-b", "w", wt)
	if got := paths.ResolveViaGitCommonDir(wt); got != "" {
		t.Fatalf("ResolveViaGitCommonDir = %s", got)
	}
	if got := paths.StoreOwnerFor(wt); got != "" {
		t.Fatalf("StoreOwnerFor = %s", got)
	}
}

func TestNonGit_KeyedByFolderAndFoundFromBelow(t *testing.T) {
	base := externalEnv(t)
	folder := filepath.Join(base, "plain", "proj")
	below := filepath.Join(folder, "src", "deep")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(folder); got != "" {
		t.Fatalf("StoreOwnerFor = %s before any store", got)
	}
	root := createStore(t, folder)
	if got := paths.CodeRoot(root); got != folder {
		t.Fatalf("CodeRoot = %s", got)
	}
	if got := resolveAt(t, folder); got != root {
		t.Fatalf("at the folder: %s", got)
	}
	if got := resolveAt(t, below); got != root {
		t.Fatalf("below the folder: %s", got)
	}
	if out := mustGit(t, root, "log", "--format=%s"); !strings.Contains(out, "Create wipnote store") {
		t.Fatalf("store log = %q", out)
	}
	if _, err := os.Stat(filepath.Join(folder, ".wipnote")); err == nil {
		t.Fatal(".wipnote added to the folder")
	}
}

func TestNonGit_RealParentStoreWinsOverNewOne(t *testing.T) {
	base := externalEnv(t)
	parent := filepath.Join(base, "dev")
	mkStore(t, parent)
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(child); got != parent {
		t.Fatalf("StoreOwnerFor = %s, want the parent %s", got, parent)
	}
}

// A store keyed by $HOME serves $HOME only: it must not take in every folder
// below it, which is the failure the $HOME rule exists to prevent.
func TestNonGit_HomeKeyedStoreDoesNotCaptureSubfolders(t *testing.T) {
	base := externalEnv(t)
	home := filepath.Join(base, "home")
	root := createStore(t, home)
	if got := paths.StoreOwnerFor(home); got != root {
		t.Fatalf("at $HOME: %s", got)
	}
	sub := filepath.Join(home, "x")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(sub); got != "" {
		t.Fatalf("subfolder resolved to %s", got)
	}
}

func TestShadowName_EncodesLikeClaudeCode(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "My Dev.dir", "my_repo-2"))
	root := paths.ShadowRootFor(repo)
	if filepath.Dir(root) != filepath.Join(base, "home", ".wipnote", "projects") {
		t.Fatalf("root %s is not under ~/.wipnote/projects", root)
	}
	name := filepath.Base(root)
	if !strings.HasPrefix(name, "-") || !strings.HasSuffix(name, "-My-Dev-dir-my-repo-2") {
		t.Fatalf("name = %s", name)
	}
	if strings.ContainsAny(name, "/ ._") {
		t.Fatalf("unencoded characters in %s", name)
	}
}

func TestShadowName_CollisionGetsHashSuffix(t *testing.T) {
	base := externalEnv(t)
	one := newRepo(t, filepath.Join(base, "a-b"))
	two := newRepo(t, filepath.Join(base, "a", "b"))
	first := paths.ShadowRootFor(one)
	if paths.ShadowRootFor(two) != first {
		t.Fatal("the two paths should encode alike before either has a store")
	}
	rootOne := createStore(t, one)
	rootTwo := createStore(t, two)
	if rootOne != first {
		t.Fatalf("first store at %s, want %s", rootOne, first)
	}
	if len(rootTwo) != len(rootOne)+9 || !strings.HasPrefix(rootTwo, rootOne+"-") {
		t.Fatalf("second store at %s, want %s-<hash8>", rootTwo, rootOne)
	}
	if paths.CodeRoot(rootOne) != one || paths.CodeRoot(rootTwo) != two {
		t.Fatal("stores swapped their code roots")
	}
	if resolveAt(t, one) != rootOne || resolveAt(t, two) != rootTwo {
		t.Fatal("repos resolve to the wrong stores")
	}

	// The second stays put when the first goes away.
	if err := os.RemoveAll(rootOne); err != nil {
		t.Fatal(err)
	}
	if got := paths.ShadowRootFor(two); got != rootTwo {
		t.Fatalf("after the first store went: %s, want %s", got, rootTwo)
	}
}

func TestShadowName_LongPathCutAndSuffixed(t *testing.T) {
	base := externalEnv(t)
	long := base
	for len(long) < 260 {
		long = filepath.Join(long, strings.Repeat("d", 60))
	}
	repo := newRepo(t, long)
	root := createStore(t, repo)
	name := filepath.Base(root)
	if len(name) != 200 {
		t.Fatalf("name is %d characters", len(name))
	}
	if !regexp.MustCompile(`-[0-9a-f]{8}$`).MatchString(name) {
		t.Fatalf("name %s has no -<hash8>", name)
	}
	if got := resolveAt(t, repo); got != root {
		t.Fatalf("resolved to %s", got)
	}
}

func TestResolve_NonGitWalkUpUnchanged(t *testing.T) {
	base := externalEnv(t)
	mkStore(t, base)
	sub := filepath.Join(base, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveAt(t, sub); got != base {
		t.Fatalf("got %s, want the walk-up result %s", got, base)
	}
}

func TestResolve_EnvAndFlagValidateShadow(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	root := createStore(t, repo)

	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	if got := resolveAt(t, t.TempDir()); got != root {
		t.Fatalf("a valid env value was ignored: %s", got)
	}
	got, err := paths.ResolveProjectDir(paths.ProjectDirOptions{ExplicitDir: repo})
	if err != nil || got != root {
		t.Fatalf("--project-dir on the code repo: got %s, %v; want the shadow %s", got, err, root)
	}

	// A shadow root whose code root is gone is refused, not used.
	moved := repo + "-moved"
	if err := os.Rename(repo, moved); err != nil {
		t.Fatal(err)
	}
	if _, err := paths.ResolveProjectDir(paths.ProjectDirOptions{ExplicitDir: root}); err == nil {
		t.Fatal("--project-dir accepted a shadow root whose code root is gone")
	}
	other := t.TempDir()
	if got := resolveAt(t, other); got == root {
		t.Fatal("env value accepted a shadow root whose code root is gone")
	}
}

func TestResolve_BrokenShadowRootIsNotAStore(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	root := paths.ShadowRootFor(repo)
	mkStore(t, root) // no project.json
	if got := resolveAt(t, repo); got == root {
		t.Fatal("resolved to a shadow root with no project.json")
	}
	// The broken folder counts as taken: the store goes to the suffixed name.
	got, created, err := paths.CreateExternalStore(repo, paths.OriginInit, nil)
	if err != nil || !created || got == root || !strings.HasPrefix(got, root+"-") {
		t.Fatalf("CreateExternalStore = %s, %v, %v; want a new %s-<hash8>", got, created, err, root)
	}
}

func TestCodeRoot_InRepoAndUnrelatedProjectFile(t *testing.T) {
	dir := t.TempDir()
	if got := paths.CodeRoot(dir); got != dir {
		t.Fatalf("got %s", got)
	}
	unrelated := `{"code_root":"/somewhere/else","name":"x"}`
	if err := os.WriteFile(filepath.Join(dir, paths.ProjectFileName), []byte(unrelated), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := paths.CodeRoot(dir); got != dir {
		t.Fatalf("a repo's own project.json was read as a shadow marker: %s", got)
	}
}

func TestCreateExternalStore_LayoutAndGit(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "My Repo"))
	seeded := ""
	root, created, err := paths.CreateExternalStore(repo, paths.OriginInit, func(store string) error {
		seeded = store
		return os.WriteFile(filepath.Join(store, "seed.txt"), []byte("x"), 0o644)
	})
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if want := filepath.Join(base, "home", ".wipnote", "projects"); filepath.Dir(root) != want {
		t.Fatalf("root %s is not under %s", root, want)
	}
	if !strings.HasSuffix(seeded, ".wipnote") {
		t.Fatalf("seed got %q", seeded)
	}
	var pf map[string]string
	data, _ := os.ReadFile(filepath.Join(root, paths.ProjectFileName))
	if err := json.Unmarshal(data, &pf); err != nil || pf["code_root"] != repo {
		t.Fatalf("project.json = %s (%v)", data, err)
	}
	// No global identity exists here, so commits must still work.
	if out := mustGit(t, root, "log", "--format=%an %s"); !strings.Contains(out, "Create wipnote store") {
		t.Fatalf("store log = %q", out)
	}
	if out := mustGit(t, root, "status", "--porcelain"); out != "" {
		t.Fatalf("new store is dirty: %q", out)
	}
	if out := mustGit(t, root, "remote"); out != "" {
		t.Fatalf("store has a remote: %q", out)
	}
	if out := mustGit(t, repo, "status", "--porcelain"); out != "" {
		t.Fatalf("code repo is dirty: %q", out)
	}
	if _, created, err := paths.CreateExternalStore(repo, paths.OriginInit, nil); err != nil || created {
		t.Fatalf("second create: created=%v err=%v", created, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(root))
	if len(entries) != 1 {
		t.Fatalf("temp folders left behind: %v", entries)
	}
}

func TestResolve_SymlinkedCwdKeepsItsSpelling(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "repo"))
	mkStore(t, repo)
	link := filepath.Join(base, "link")
	if err := os.Symlink(repo, link); err != nil {
		t.Fatal(err)
	}
	if got := resolveAt(t, link); got != link {
		t.Fatalf("got %s, want the given spelling %s", got, link)
	}
}
