package paths_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// superWithSubmodule makes super/ with a submodule at super/lib.
func superWithSubmodule(t *testing.T, base string) (super, lib string) {
	t.Helper()
	src := newRepo(t, filepath.Join(base, "libsrc"))
	super = newRepo(t, filepath.Join(base, "super"))
	mustGit(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "lib")
	mustGit(t, super, "commit", "-q", "-m", "add lib")
	return super, filepath.Join(super, "lib")
}

// Finding 1: a superproject's in-repo store is found from inside a submodule.
func TestReview1_SubmoduleFindsSuperprojectStore(t *testing.T) {
	super, lib := superWithSubmodule(t, externalEnv(t))
	mkStore(t, super)
	deep := filepath.Join(lib, "pkg")
	mkdirs(t, deep)
	if got := resolveAt(t, deep); got != super {
		t.Fatalf("resolved to %s, want the superproject %s", got, super)
	}
	if got := paths.StoreOwnerFor(lib); got != super {
		t.Fatalf("StoreOwnerFor = %q, want %s", got, super)
	}
}

// Finding 1: with no store, a submodule shares its superproject's store.
func TestReview1_SubmoduleSharesSuperprojectShadow(t *testing.T) {
	super, lib := superWithSubmodule(t, externalEnv(t))
	root, _, err := paths.CreateExternalStore(lib, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := paths.ShadowRootFor(super); root != want {
		t.Fatalf("store from the submodule at %s, want the superproject's %s", root, want)
	}
	if got := paths.StoreOwnerFor(super); got != root {
		t.Fatalf("superproject resolves to %q", got)
	}
}

// bareLayout makes proj/.bare with a worktree at proj/main.
func bareLayout(t *testing.T, base string) (proj, main string) {
	t.Helper()
	src := newRepo(t, filepath.Join(base, "src"))
	proj = filepath.Join(base, "proj")
	mkdirs(t, proj)
	mustGit(t, proj, "clone", "-q", "--bare", src, ".bare")
	main = filepath.Join(proj, "main")
	mustGit(t, filepath.Join(proj, ".bare"), "worktree", "add", "-q", main)
	return proj, main
}

// Finding 3: every resolver agrees on a bare repo's worktree.
func TestReview3_BareWorktreeResolversAgree(t *testing.T) {
	proj, main := bareLayout(t, externalEnv(t))
	mkStore(t, proj)
	if got := paths.ResolveViaGitCommonDir(main); got != proj {
		t.Fatalf("ResolveViaGitCommonDir = %q", got)
	}
	if got := paths.StoreOwnerFor(main); got != proj {
		t.Fatalf("StoreOwnerFor = %q, want %s", got, proj)
	}
}

// Finding 3: with no store, init from a bare repo's worktree makes the store
// resolution then uses, shared by every worktree.
func TestReview3_BareWorktreeStoreIsUsed(t *testing.T) {
	proj, main := bareLayout(t, externalEnv(t))
	root, _, err := paths.CreateExternalStore(main, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(main); got != root {
		t.Fatalf("StoreOwnerFor = %q, want the store just made %s", got, root)
	}
	other := filepath.Join(proj, "other")
	mustGit(t, filepath.Join(proj, ".bare"), "worktree", "add", "-q", "-b", "o", other)
	if got := paths.StoreOwnerFor(other); got != root {
		t.Fatalf("second worktree resolves to %q, want %s", got, root)
	}
}

// Finding 4: a launcher's automatic store high in the tree does not take in the
// non-git folders below it.
func TestReview4_LauncherStoreDoesNotCaptureBelow(t *testing.T) {
	base := externalEnv(t)
	high := filepath.Join(base, "dev")
	child := filepath.Join(high, "child")
	mkdirs(t, child)
	root, _, err := paths.CreateExternalStore(high, paths.OriginLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(child); got != "" {
		t.Fatalf("child adopted the ancestor's automatic store %s", got)
	}
	if got := paths.StoreOwnerFor(high); got != root {
		t.Fatalf("the launch folder itself lost its store: %q", got)
	}

	// `wipnote init` there makes it explicit, and from then on it serves below.
	if changed, err := paths.MarkStoreExplicit(root); err != nil || !changed {
		t.Fatalf("MarkStoreExplicit: %v, %v", changed, err)
	}
	if got := paths.StoreOwnerFor(child); got != root {
		t.Fatalf("child after init: %q, want %s", got, root)
	}
	if out := mustGit(t, root, "status", "--porcelain"); out != "" {
		t.Fatalf("store left dirty: %q", out)
	}
	if changed, _ := paths.MarkStoreExplicit(root); changed {
		t.Fatal("MarkStoreExplicit changed an explicit store")
	}
}

// Finding 4: an explicit store keyed by a non-git folder serves folders below.
func TestReview4_ExplicitStoreServesBelow(t *testing.T) {
	base := externalEnv(t)
	high := filepath.Join(base, "dev")
	child := filepath.Join(high, "a", "b")
	mkdirs(t, child)
	root, _, err := paths.CreateExternalStore(high, paths.OriginInit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(child); got != root {
		t.Fatalf("child = %q, want %s", got, root)
	}
}

// Finding 4: in a git repo the origin does not matter; the repo is the project.
func TestReview4_LauncherStoreServesWholeRepo(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	deep := filepath.Join(repo, "x", "y")
	mkdirs(t, deep)
	root, _, err := paths.CreateExternalStore(repo, paths.OriginLauncher, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(deep); got != root {
		t.Fatalf("deep = %q, want %s", got, root)
	}
}

// Finding 5: the app home is guarded like $HOME.
func TestReview5_AppHomeIsNeverAProject(t *testing.T) {
	base := externalEnv(t)
	app := filepath.Join(base, "home", ".wipnote")
	projects := filepath.Join(app, "projects")
	mkdirs(t, projects, filepath.Join(app, ".wipnote"))
	if paths.IsStoreOwner(app) {
		t.Error("~/.wipnote/.wipnote counts as a store")
	}
	if paths.StoreCreationAllowed(app) {
		t.Error("StoreCreationAllowed(~/.wipnote) = true")
	}
	inside := filepath.Join(projects, "-some-project", "x")
	mkdirs(t, inside)
	if got := paths.StoreOwnerFor(inside); got != "" {
		t.Errorf("a folder under ~/.wipnote/projects resolved to %s", got)
	}
	for _, dir := range []string{app, projects} {
		if _, _, err := paths.CreateExternalStore(dir, paths.OriginInit, nil); err == nil {
			t.Errorf("CreateExternalStore(%s) succeeded", dir)
		}
	}
}

// Finding 6: a broken folder at the encoded name sends another project to the
// suffixed name.
func TestReview6_BrokenPrimaryIsTaken(t *testing.T) {
	base := externalEnv(t)
	a := newRepo(t, filepath.Join(base, "a-b"))
	b := newRepo(t, filepath.Join(base, "a", "b"))
	broken := paths.ShadowRootFor(a)
	mkStore(t, broken) // no project.json
	root, created, err := paths.CreateExternalStore(b, paths.OriginInit, nil)
	if err != nil || !created {
		t.Fatalf("created=%v err=%v", created, err)
	}
	if root == broken || root[:len(broken)] != broken {
		t.Fatalf("store at %s, want %s-<hash8>", root, broken)
	}
}
