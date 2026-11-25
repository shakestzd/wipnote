package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

// OriginMoved marks a shadow root that `wipnote store move` filled from an
// in-repo store (feat-e383e3d0). It outranks a .wipnote/ in the code root:
// checking out a commit from before the move puts that folder back, and the
// store must not flip to the stale copy.
const OriginMoved = "moved"

// IsMovedStore reports a shadow root made by `wipnote store move`.
func IsMovedStore(owner string) bool {
	pf, ok := readProjectFile(owner)
	return ok && pf.Origin == OriginMoved
}

// movedStoreOver is the moved shadow root that replaces an in-repo store at
// dir, or "". The main checkout and a non-git folder cost file reads only. A
// linked worktree or submodule keeps .git as a file, and a store below a
// repo's root has no .git of its own. For both, only git knows the main root
// the store is keyed by.
func movedStoreOver(dir string) string {
	if dir == "" || IsExternalStore(dir) {
		return ""
	}
	if shadow := ShadowRootFor(dir); IsMovedStore(shadow) && validStoreOwner(shadow) {
		return shadow
	}
	if !rootNeedsGit(dir) {
		return ""
	}
	if s, ok := scopeOf(dir); ok {
		if shadow := s.shadow(); IsMovedStore(shadow) {
			return shadow
		}
	}
	return ""
}

// rootNeedsGit reports whether only git can name the root dir's store is keyed
// by. It reads files and starts no process. A main checkout keeps .git as a
// folder, and its own shadow root was already checked. A folder with no .git
// needs git only when a folder above it is a checkout: without this, a command
// run in repo/sub kept using repo/sub/.wipnote after that store had moved.
func rootNeedsGit(dir string) bool {
	if fi, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
		return !fi.IsDir()
	}
	for d := filepath.Dir(dir); ; d = filepath.Dir(d) {
		if _, err := os.Lstat(filepath.Join(d, ".git")); err == nil {
			return true
		}
		if d == filepath.Dir(d) {
			return false
		}
	}
}

// MovedFrom is where the moved store at owner lived in each checkout, relative
// to the checkout's top. It is ".wipnote" unless the store was nested.
func MovedFrom(owner string) string {
	if pf, ok := readProjectFile(owner); ok && pf.MovedFrom != "" {
		return filepath.FromSlash(pf.MovedFrom)
	}
	return ".wipnote"
}

// MoveStoreToExternal makes the shadow root for dir's project as a moved store
// and returns it. movedFrom is the old store folder relative to the checkout's
// top. seed copies the old store's files in. A shadow root that another origin
// made is refused: its records are not the project's in-repo ones.
func MoveStoreToExternal(dir, movedFrom string, seed func(storeDir string) error) (string, error) {
	root, created, err := createExternalStore(dir, projectFile{Origin: OriginMoved, MovedFrom: filepath.ToSlash(movedFrom)}, seed)
	if err != nil {
		return "", err
	}
	if !created && !IsMovedStore(root) {
		return "", fmt.Errorf("an app store for %s already exists at %s and no move made it; remove it or merge it by hand first", dir, root)
	}
	return root, nil
}
