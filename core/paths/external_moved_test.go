package paths_test

import (
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

// A store made by `wipnote store move` outranks the in-repo folder, which an
// old commit can put back, from the main checkout and from a linked worktree.
func TestResolve_MovedStoreWinsOverInRepoStore(t *testing.T) {
	base := externalEnv(t)
	repo := newRepo(t, filepath.Join(base, "repo"))
	root, err := paths.MoveStoreToExternal(repo, ".wipnote", nil)
	if err != nil {
		t.Fatal(err)
	}
	mkStore(t, repo)
	wt := filepath.Join(base, "wt")
	mustGit(t, repo, "worktree", "add", "-q", wt)
	mkStore(t, wt)
	for _, dir := range []string{repo, wt} {
		if got := resolveAt(t, dir); got != root {
			t.Errorf("ResolveProjectDir(%s) = %s, want the moved store %s", dir, got, root)
		}
		if got := paths.StoreOwnerFor(dir); got != root {
			t.Errorf("StoreOwnerFor(%s) = %s, want the moved store %s", dir, got, root)
		}
	}
	if owner, code := paths.Roots(repo); owner != root || code != repo {
		t.Errorf("Roots = %s, %s; want %s, %s", owner, code, root, repo)
	}
	if again, err := paths.MoveStoreToExternal(repo, ".wipnote", nil); err != nil || again != root {
		t.Errorf("a second move = %s, %v; want %s", again, err, root)
	}
}

func TestMoveStoreToExternal_RefusesAnotherOriginsStore(t *testing.T) {
	repo := newRepo(t, filepath.Join(externalEnv(t), "repo"))
	createStore(t, repo)
	if _, err := paths.MoveStoreToExternal(repo, ".wipnote", nil); err == nil {
		t.Fatal("moved over a store that `wipnote init` made")
	}
}
