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
// linked worktree or submodule keeps .git as a file, and only git knows the
// main root its store is keyed by.
func movedStoreOver(dir string) string {
	if dir == "" || IsExternalStore(dir) {
		return ""
	}
	if shadow := ShadowRootFor(dir); IsMovedStore(shadow) && validStoreOwner(shadow) {
		return shadow
	}
	if fi, err := os.Lstat(filepath.Join(dir, ".git")); err != nil || fi.IsDir() {
		return ""
	}
	if s, ok := scopeOf(dir); ok {
		if shadow := s.shadow(); IsMovedStore(shadow) {
			return shadow
		}
	}
	return ""
}

// MoveStoreToExternal makes the shadow root for the project at dir and marks it
// OriginMoved. seed fills the new .wipnote/ before the first store commit. A
// store an earlier move made is returned as it is. A shadow root from any
// other origin is refused: it may hold records the copy would hide.
func MoveStoreToExternal(dir string, seed func(storeDir string) error) (string, error) {
	root, created, err := CreateExternalStore(dir, OriginMoved, seed)
	if err != nil {
		return "", err
	}
	if !created && !IsMovedStore(root) {
		return "", fmt.Errorf("an app store for %s already exists at %s and no move made it; remove it or merge it by hand first", dir, root)
	}
	return root, nil
}
