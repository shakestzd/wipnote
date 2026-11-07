package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/registry"
)

// externalProject is a code repo nested under a folder that holds a .wipnote,
// with HOME in scratch space: no test here can reach the real ~/.wipnote.
type externalProject struct {
	base, parent, code string
}

func newExternalProject(t *testing.T) externalProject {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(base, "home"))
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_SESSION_ID", "")
	saved := projectDirFlag
	projectDirFlag = ""
	t.Cleanup(func() { projectDirFlag = saved })

	p := externalProject{base: base, parent: filepath.Join(base, "dev"), code: filepath.Join(base, "dev", "code")}
	for _, d := range []string{filepath.Join(base, "home"), filepath.Join(p.parent, ".wipnote", "features"), p.code} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	extGit(t, p.code, "init", "-q", "-b", "main")
	p.write(t, "main.go", "package main\n")
	extGit(t, p.code, "add", "main.go")
	extGit(t, p.code, "commit", "-q", "-m", "first")
	t.Chdir(p.code)
	return p
}

func extGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (p externalProject) write(t *testing.T, rel, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.code, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (p externalProject) store(t *testing.T) string {
	t.Helper()
	root, err := ensureExternalStore(p.code, paths.OriginInit)
	if err != nil || root == "" {
		t.Fatalf("ensureExternalStore: %q, %v", root, err)
	}
	return root
}

func (p externalProject) assertCodeRepoClean(t *testing.T) {
	t.Helper()
	if out := extGit(t, p.code, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Fatalf("code repo is dirty:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote")); err == nil {
		t.Fatal(".wipnote appeared in the code repo")
	}
}

func (p externalProject) assertNoStoreMade(t *testing.T) {
	t.Helper()
	p.assertCodeRepoClean(t)
	if _, err := os.Stat(paths.ProjectsDir()); err == nil {
		t.Fatal("a read-only command created the projects folder")
	}
}

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := buildRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.Execute()
	return out.String(), err
}

// An item in the store, the way the store is laid out for a real project.
func newStoreFeature(t *testing.T, storeRoot, title string) string {
	t.Helper()
	p, err := workitem.Open(filepath.Join(storeRoot, ".wipnote"), "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	n, err := p.Features.Create(title, workitem.FeatWithTrack("trk-00000000"))
	if err != nil {
		t.Fatal(err)
	}
	return n.ID
}

// Commands that read a store use the repo's app store, made on first use, and
// leave the repo itself untouched.
func TestReadOnlyCommands_UseAppStoreAndLeaveRepoClean(t *testing.T) {
	p := newExternalProject(t)
	for _, args := range [][]string{{"status"}, {"wip"}, {"snapshot", "--summary"}, {"who"}, {"feature", "list"}} {
		_, _ = runCLI(t, args...)
		p.assertCodeRepoClean(t)
	}
	if n := projectsCount(t); n != 1 {
		t.Fatalf("%d stores after five commands, want 1", n)
	}
	if got := paths.ExternalStoreFor(p.code); got == "" || paths.CodeRoot(got) != p.code {
		t.Fatalf("store = %q", got)
	}
}

func TestStoreCreation_NoSideEffectsInCodeRepo(t *testing.T) {
	p := newExternalProject(t)
	writeLaunchMarker("go", p.code)
	p.assertNoStoreMade(t)

	root := p.store(t)
	if got := paths.CodeRoot(root); got != p.code {
		t.Fatalf("CodeRoot = %s, want %s", got, p.code)
	}
	wd, err := findWipnoteDir()
	if err != nil || wd != filepath.Join(root, ".wipnote") {
		t.Fatalf("findWipnoteDir = %s, %v", wd, err)
	}
	writeLaunchMarker("go", root)
	if _, err := os.Stat(filepath.Join(root, ".wipnote", ".launch-mode")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".wipnote", ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if out := extGit(t, root, "status", "--porcelain"); strings.Contains(out, "project.json") {
		t.Fatalf("project.json not committed:\n%s", out)
	}
	p.assertCodeRepoClean(t)
}

func TestLauncher_RunsInCodeRootWithStoreAsWipnoteRoot(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	sub := filepath.Join(p.code, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	child, wroot, err := claudeLaunchDirs("")
	if err != nil || child != p.code || wroot != root {
		t.Fatalf("claudeLaunchDirs = %s, %s, %v; want child %s and root %s", child, wroot, err, p.code, root)
	}
	opts := LaunchOpts{ProjectRoot: child, WipnoteRoot: wroot}
	if got := claudeEnvProjectDir(opts); got != root {
		t.Fatalf("WIPNOTE_PROJECT_DIR = %s, want the shadow root", got)
	}
	if got := launchMarkerRoot(opts); got != root {
		t.Fatalf("launch marker goes to %s", got)
	}
	args := strings.Join(buildClaudeArgs(opts, ""), " ")
	if !strings.Contains(args, "--add-dir="+root) {
		t.Fatalf("claude args have no store access: %s", args)
	}
	if strings.Contains(strings.Join(buildClaudeArgs(LaunchOpts{ProjectRoot: p.parent}, ""), " "), "--add-dir") {
		t.Fatal("--add-dir passed without an external store")
	}

	// A continue-style launch that sets no wipnote root gets it at the chokepoint.
	if got := externalWipnoteRoot("", p.code); got != root {
		t.Fatalf("externalWipnoteRoot = %s", got)
	}
	if got := launchStateRoot(root, p.code); got != root {
		t.Fatalf("launcher state goes to %s", got)
	}
}

func TestLauncher_WorkDirFindsStoreNeverAParent(t *testing.T) {
	p := newExternalProject(t)
	t.Chdir(p.base)
	if _, _, err := claudeLaunchDirs(p.code); err == nil {
		t.Fatalf("--work-dir landed on the parent store at %s", p.parent)
	}
	root := p.store(t)
	child, wroot, err := claudeLaunchDirs(p.code)
	if err != nil || child != p.code || wroot != root {
		t.Fatalf("claudeLaunchDirs = %s, %s, %v", child, wroot, err)
	}
}

func TestLauncherMayCreateStore_OnlyRealTopLevelLaunches(t *testing.T) {
	root := buildRoot()
	find := func(args ...string) bool {
		c, _, err := root.Find(args)
		if err != nil {
			t.Fatal(err)
		}
		return launcherMayCreateStore(c)
	}
	for _, name := range []string{"claude", "codex", "yolo", "antigravity"} {
		if !find(name) {
			t.Errorf("%s may not create a store", name)
		}
	}
	for _, name := range []string{"status", "wip", "init", "feature"} {
		if find(name) {
			t.Errorf("%s may create a store", name)
		}
	}
}

func TestInit_DefaultMakesAppOwnedStore(t *testing.T) {
	p := newExternalProject(t)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	root := paths.ExternalStoreFor(p.code)
	if root == "" || filepath.Dir(root) != paths.ProjectsDir() {
		t.Fatalf("shadow root = %q", root)
	}
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "sessions"} {
		if _, err := os.Stat(filepath.Join(root, ".wipnote", sub)); err != nil {
			t.Error(err)
		}
	}
	p.assertCodeRepoClean(t)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

func TestInit_InRepoFlagStaysInRepoAndWins(t *testing.T) {
	p := newExternalProject(t)
	if _, err := runCLI(t, "init", "--in-repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.code, ".wipnote", "features")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.ProjectsDir()); err == nil {
		t.Fatal("--in-repo made a projects folder")
	}
	// A later plain init keeps the in-repo store and makes nothing.
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.ProjectsDir()); err == nil {
		t.Fatal("plain init made a store beside an in-repo one")
	}
}

// Finding 2: --in-repo refuses a project an app-owned store serves, naming it,
// from the main checkout and from a linked worktree.
func TestInit_InRepoRefusedWhenAppOwnedStoreServes(t *testing.T) {
	p := newExternalProject(t)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	root := paths.ExternalStoreFor(p.code)
	wt := filepath.Join(p.base, "wt")
	extGit(t, p.code, "worktree", "add", "-q", "-b", "w", wt)
	for _, dir := range []string{p.code, wt} {
		t.Chdir(dir)
		_, err := runCLI(t, "init", "--in-repo")
		if err == nil || !strings.Contains(err.Error(), root) {
			t.Fatalf("from %s: --in-repo = %v, want a refusal naming %s", dir, err, root)
		}
		if _, serr := os.Stat(filepath.Join(dir, ".wipnote")); serr == nil {
			t.Fatalf("from %s: .wipnote created", dir)
		}
	}
}

// Finding 5: --in-repo is refused in the app home.
func TestInit_InRepoRefusedInAppHome(t *testing.T) {
	newExternalProject(t)
	app := paths.DataDir()
	if err := os.MkdirAll(app, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(app)
	if _, err := runCLI(t, "init", "--in-repo"); err == nil {
		t.Fatal("--in-repo made a project of ~/.wipnote")
	}
	if _, err := os.Stat(filepath.Join(app, ".wipnote")); err == nil {
		t.Fatal("~/.wipnote/.wipnote created")
	}
}

// Finding 4: a launcher's store in a non-git folder is made explicit by init.
func TestInit_MarksLauncherStoreExplicit(t *testing.T) {
	p := newExternalProject(t)
	plain := filepath.Join(p.base, "plain")
	child := filepath.Join(plain, "child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(plain)
	if err := ensureLauncherStore(); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(child); got != "" {
		t.Fatalf("child adopted the launcher's store %s", got)
	}
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(child); got == "" || paths.CodeRoot(got) != plain {
		t.Fatalf("child after init = %q", got)
	}
}

func TestInit_ExternalFlagIsGone(t *testing.T) {
	newExternalProject(t)
	if _, err := runCLI(t, "init", "--external"); err == nil {
		t.Fatal("--external was accepted")
	}
}

func TestInit_NonGitFolderKeyedByFolder(t *testing.T) {
	p := newExternalProject(t)
	plain := filepath.Join(p.base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(plain)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	owner := paths.StoreOwnerFor(plain)
	if !paths.IsExternalStore(owner) || paths.CodeRoot(owner) != plain {
		t.Fatalf("owner = %q", owner)
	}
	if _, err := os.Stat(filepath.Join(plain, ".wipnote")); err == nil {
		t.Fatal(".wipnote added to the folder")
	}
}

func TestInit_NonGitHonoursRealParentStore(t *testing.T) {
	p := newExternalProject(t)
	child := filepath.Join(p.parent, "plain-child")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	if _, err := runCLI(t, "init"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(paths.ProjectsDir()); err == nil {
		t.Fatal("made a store beside the parent project's")
	}
	if got := paths.StoreOwnerFor(child); got != p.parent {
		t.Fatalf("owner = %s", got)
	}
}

func TestInit_InRepoRefusedAtHome(t *testing.T) {
	p := newExternalProject(t)
	home := filepath.Join(p.base, "home")
	t.Chdir(home)
	if _, err := runCLI(t, "init", "--in-repo"); err == nil {
		t.Fatal("--in-repo made a project of ~/.wipnote")
	}
	if _, err := os.Stat(filepath.Join(home, ".wipnote", "features")); err == nil {
		t.Fatal("store folders written into ~/.wipnote")
	}
}

// $HOME's .wipnote is the application's: no command may treat it as a store,
// from $HOME or from a plain folder below it.
// $HOME is a plain folder: a command there uses the app store keyed by $HOME,
// never the store-shaped ~/.wipnote, and a folder below gets a store of its own.
// A command inside ~/.wipnote still refuses.
func TestFindWipnoteDir_HomeIsAPlainFolder(t *testing.T) {
	p := newExternalProject(t)
	home := filepath.Join(p.base, "home")
	below := filepath.Join(home, "plain")
	app := filepath.Join(home, ".wipnote")
	for _, d := range []string{filepath.Join(app, "features"), below} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(home)
	got, err := findWipnoteDir()
	if err != nil || got != filepath.Join(paths.ShadowRootFor(home), ".wipnote") {
		t.Fatalf("at $HOME: %s, %v", got, err)
	}
	t.Chdir(below)
	if got2, err := findWipnoteDir(); err != nil || got2 == got || paths.CodeRoot(filepath.Dir(got2)) != below {
		t.Fatalf("below $HOME: %s, %v", got2, err)
	}
	t.Chdir(filepath.Join(app, "features"))
	if got3, err := findWipnoteDir(); err == nil {
		t.Fatalf("inside ~/.wipnote: %s", got3)
	}
	if _, err := os.Stat(filepath.Join(app, ".wipnote")); err == nil {
		t.Fatal("~/.wipnote/.wipnote created")
	}
	if got := withoutHomeEntry([]registry.Entry{{ProjectDir: home}, {ProjectDir: p.code}}); len(got) != 1 || got[0].ProjectDir != p.code {
		t.Fatalf("withoutHomeEntry = %+v", got)
	}
}

func plainFolder(t *testing.T, p externalProject, parts ...string) string {
	t.Helper()
	dir := filepath.Join(append([]string{p.base, "plain"}, parts...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func projectsCount(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(paths.ProjectsDir())
	if err != nil {
		return 0
	}
	return len(entries)
}

// Owner decision: a command in a plain folder makes and uses the folder's app
// store, adds nothing to the folder, reuses it next time, and a child folder
// does not adopt it.
func TestPlainFolderCommand_MakesAndReusesAppStore(t *testing.T) {
	p := newExternalProject(t)
	plain := plainFolder(t, p)
	child := plainFolder(t, p, "child")
	t.Chdir(plain)
	if _, err := runCLI(t, "feature", "create", "first", "--description", "d", "--track", "trk-00000000"); err != nil {
		t.Fatalf("feature create in a plain folder: %v", err)
	}
	owner := paths.StoreOwnerFor(plain)
	if !paths.IsExternalStore(owner) || paths.CodeRoot(owner) != plain {
		t.Fatalf("store = %q", owner)
	}
	if items, _ := filepath.Glob(filepath.Join(owner, ".wipnote", "features", "*.html")); len(items) != 1 {
		t.Fatalf("features in the store: %v", items)
	}
	if _, err := os.Stat(filepath.Join(plain, ".wipnote")); err == nil {
		t.Fatal(".wipnote added to the plain folder")
	}

	if _, err := runCLI(t, "feature", "list"); err != nil {
		t.Fatal(err)
	}
	if n := projectsCount(t); n != 1 {
		t.Fatalf("%d stores after the second command, want 1", n)
	}
	if got, err := findWipnoteDir(); err != nil || filepath.Dir(got) != owner {
		t.Fatalf("second command resolved to %s, %v", got, err)
	}

	t.Chdir(child)
	got, err := findWipnoteDir()
	if err != nil || filepath.Dir(got) == owner {
		t.Fatalf("child adopted the parent's store: %s, %v", got, err)
	}
	if n := projectsCount(t); n != 2 {
		t.Fatalf("%d stores after the child's command, want 2", n)
	}
}

// Code that runs wherever a harness or shell is (pre-run, status line, MCP)
// never makes a store.
func TestFindExistingWipnoteDir_MakesNothing(t *testing.T) {
	p := newExternalProject(t)
	t.Chdir(plainFolder(t, p))
	if got, err := findExistingWipnoteDir(); err == nil {
		t.Fatalf("got %s", got)
	}
	for _, args := range [][]string{{"statusline"}, {"version"}} {
		_, _ = runCLI(t, args...)
	}
	if n := projectsCount(t); n != 0 {
		t.Fatalf("%d stores made", n)
	}
}

// Owner decision: a command in a git repo with no store makes the repo's app
// store (keyed by the main root, origin launcher), reuses it, leaves the repo's
// git status clean, and a linked worktree uses the same store.
func TestFindWipnoteDir_GitRepoMakesAndReusesAppStore(t *testing.T) {
	p := newExternalProject(t)
	sub := filepath.Join(p.code, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	if _, err := runCLI(t, "feature", "create", "first", "--description", "d", "--track", "trk-00000000"); err != nil {
		t.Fatalf("feature create in a repo with no store: %v", err)
	}
	owner := paths.ExternalStoreFor(p.code)
	if owner != paths.ShadowRootFor(p.code) || paths.CodeRoot(owner) != p.code {
		t.Fatalf("store = %q, want the one keyed by the main root", owner)
	}
	if items, _ := filepath.Glob(filepath.Join(owner, ".wipnote", "features", "*.html")); len(items) != 1 {
		t.Fatalf("features in the store: %v", items)
	}
	if data, err := os.ReadFile(filepath.Join(owner, paths.ProjectFileName)); err != nil || !strings.Contains(string(data), `"origin": "launcher"`) {
		t.Fatalf("project.json = %s, %v", data, err)
	}
	p.assertCodeRepoClean(t)

	t.Chdir(p.code)
	if got, err := findWipnoteDir(); err != nil || filepath.Dir(got) != owner {
		t.Fatalf("second command: %s, %v", got, err)
	}
	wt := filepath.Join(p.base, "wt")
	extGit(t, p.code, "worktree", "add", "-q", "-b", "w", wt)
	t.Chdir(wt)
	if got, err := findWipnoteDir(); err != nil || filepath.Dir(got) != owner {
		t.Fatalf("linked worktree: %s, %v", got, err)
	}
	if n := projectsCount(t); n != 1 {
		t.Fatalf("%d stores, want 1", n)
	}
	p.assertCodeRepoClean(t)
	if out := extGit(t, wt, "status", "--porcelain", "--untracked-files=all"); out != "" {
		t.Fatalf("worktree is dirty:\n%s", out)
	}
}

// Passive callers make nothing in a git repo either.
func TestFindExistingWipnoteDir_MakesNothingInGitRepo(t *testing.T) {
	p := newExternalProject(t)
	if got, err := findExistingWipnoteDir(); err == nil {
		t.Fatalf("got %s", got)
	}
	_, _ = runCLI(t, "statusline")
	if n := projectsCount(t); n != 0 {
		t.Fatalf("%d stores made", n)
	}
	p.assertCodeRepoClean(t)
}

func TestLauncher_NonGitFolderGetsStoreKeyedByFolder(t *testing.T) {
	p := newExternalProject(t)
	plain := filepath.Join(p.base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(plain)
	if err := ensureLauncherStore(); err != nil {
		t.Fatal(err)
	}
	owner := paths.StoreOwnerFor(plain)
	if !paths.IsExternalStore(owner) {
		t.Fatalf("owner = %q", owner)
	}
	child, wroot, err := claudeLaunchDirs("")
	if err != nil || child != plain || wroot != owner {
		t.Fatalf("claudeLaunchDirs = %s, %s, %v", child, wroot, err)
	}
	if _, err := os.Stat(filepath.Join(plain, ".wipnote")); err == nil {
		t.Fatal(".wipnote added to the folder")
	}
}

func TestDirtyGate_ReadsCodeRepoUnderExternalStore(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	wd := filepath.Join(root, ".wipnote")

	ev := newCompletionEvidence(wd, "feat-11111111", nil)
	if ev.repoRoot != p.code {
		t.Fatalf("evidence reads %s, want the code repo %s", ev.repoRoot, p.code)
	}

	// With a clean store and a dirty code repo the note must name the dirty file.
	p.write(t, "main.go", "package main\n\nfunc main() {}\n")
	note, err := checkUncommittedSourceCompleteGate(ev, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(note, "main.go") {
		t.Fatalf("the dirty code repo did not reach the gate; note = %q", note)
	}

	// A commit that names the item makes main.go its own file: the gate refuses.
	p.write(t, "own.go", "package main\n")
	extGit(t, p.code, "add", "own.go")
	extGit(t, p.code, "commit", "-q", "-m", "work on feat-11111111")
	p.write(t, "own.go", "package main\n\nvar x int\n")
	ev = newCompletionEvidence(wd, "feat-11111111", nil)
	if len(ev.linkedCommits()) == 0 {
		t.Fatal("commit evidence did not come from the code repo")
	}
	if _, err := checkUncommittedSourceCompleteGate(ev, false); err == nil || !strings.Contains(err.Error(), "own.go") {
		t.Fatalf("the dirty gate did not trip: %v", err)
	}
}

func TestDirtyGate_DirtyStoreDoesNotCount(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	if err := os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	note, err := checkUncommittedSourceCompleteGate(newCompletionEvidence(filepath.Join(root, ".wipnote"), "feat-22222222", nil), false)
	if err != nil || strings.Contains(note, "stray.txt") {
		t.Fatalf("the gate read the store's tree: %q, %v", note, err)
	}
}

func TestMergedGate_ReadsCodeRepoUnderExternalStore(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	wd := filepath.Join(root, ".wipnote")
	id := newStoreFeature(t, root, "merged gate")

	// origin/main stops before the item's commit, so the commit is unmerged.
	upstream := extGit(t, p.code, "rev-parse", "HEAD")
	extGit(t, p.code, "update-ref", "refs/remotes/origin/main", upstream)
	extGit(t, p.code, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	p.write(t, "own.go", "package main\n")
	extGit(t, p.code, "add", "own.go")
	extGit(t, p.code, "commit", "-q", "-m", "work on "+id)

	wp, err := workitem.Open(wd, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer wp.Close()
	ev := newCompletionEvidence(wd, id, nil)
	if err := checkMergedUpstreamCompleteGate(wp.Features.Collection, ev, "feature", id, ""); err == nil {
		t.Fatal("the merged gate passed on a commit that is not upstream: it read the wrong repo")
	}

	extGit(t, p.code, "update-ref", "refs/remotes/origin/main", "HEAD")
	ev = newCompletionEvidence(wd, id, nil)
	if err := checkMergedUpstreamCompleteGate(wp.Features.Collection, ev, "feature", id, ""); err != nil {
		t.Fatalf("the merged gate refused a commit now upstream: %v", err)
	}
}

func TestLinkCommit_ResolvesInCodeRepo(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	id := newStoreFeature(t, root, "link commit")
	sha := extGit(t, p.code, "rev-parse", "HEAD")
	if err := runLinkCommit("feature", id, sha); err != nil {
		t.Fatalf("a code repo commit did not link: %v", err)
	}
	node, err := htmlparse.ParseFile(filepath.Join(root, ".wipnote", "features", id+".html"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasCommitEdge(node, sha) {
		t.Fatalf("no committed_in edge for %s", sha)
	}
	storeSHA := extGit(t, root, "rev-parse", "HEAD")
	if err := runLinkCommit("feature", id, storeSHA); err == nil {
		t.Fatal("a store commit was linked as if it were project history")
	}
}

func TestReindexTrailers_ReadCodeRepoUnderExternalStore(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	p.write(t, "t.go", "package main\n")
	extGit(t, p.code, "add", "t.go")
	extGit(t, p.code, "commit", "-q", "-m", "with trailer\n\nRefs: feat-33333333")
	hash := extGit(t, p.code, "rev-parse", "HEAD")

	database, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var out bytes.Buffer
	if _, err := rebuildFullProjection(database, filepath.Join(root, ".wipnote"), false, &out, &out); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := database.QueryRow(`SELECT COUNT(*) FROM git_commits WHERE commit_hash = ? AND feature_id = ?`, hash, "feat-33333333").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("a trailer in a code repo commit produced %d rows", n)
	}
}

func TestFileAttribution_AnchorsOnCodeRepo(t *testing.T) {
	p := newExternalProject(t)
	p.store(t)
	got, ok := paths.NormalizeToRepoRelative(filepath.Join(p.code, "main.go"), "")
	if !ok || got != "main.go" {
		t.Fatalf("attribution = %q, want main.go", got)
	}
}

func TestSelfHeal_DoesNotRepairAgainstTheStore(t *testing.T) {
	p := newExternalProject(t)
	root := p.store(t)
	wt := filepath.Join(p.base, "wt")
	extGit(t, p.code, "worktree", "add", "-q", "-b", "w", wt)
	gitFile := filepath.Join(wt, ".git")
	stale := "gitdir: /nowhere/.git/worktrees/wt\n"
	if err := os.WriteFile(gitFile, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	t.Chdir(wt)
	selfHealGitdirIfStale()
	data, err := os.ReadFile(gitFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), root) {
		t.Fatalf("worktree pointer rewritten into the store: %s", data)
	}
	if !strings.Contains(string(data), filepath.Join(p.code, ".git", "worktrees", "wt")) {
		t.Fatalf("worktree pointer not repaired against the code repo: %s", data)
	}
}

func TestDelegateContainer_RefusedForExternalStore(t *testing.T) {
	p := newExternalProject(t)
	p.store(t)
	_, err := runCLI(t, "delegate", "spawn", "--container", "--harness", "claude", "--brief", "b", "w1")
	if err == nil || !strings.Contains(err.Error(), "external store") {
		t.Fatalf("spawn --container = %v, want the external-store refusal", err)
	}
}

func TestIsStorePath(t *testing.T) {
	hg := "/x/.wipnote"
	for path, want := range map[string]bool{".wipnote/features/a.html": true, "/x/.wipnote/a": true, "main.go": false, "/y/main.go": false, ".wipnoterc": false} {
		if got := isStorePath(hg, path); got != want {
			t.Errorf("isStorePath(%q) = %v", path, got)
		}
	}
}
