package paths

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Making and marking app-owned stores. Layout and resolution are in external.go.

// CreateExternalStore makes the shadow root for dir and returns it, with created
// true when this call made it. It is keyed by dir's repo (the main root) or, when
// dir is in no repo, by dir itself; origin (OriginInit or OriginLauncher) goes in
// project.json. seed fills the new .wipnote/ before the first store commit. The
// root is built under a temp name and renamed, so an existing root is always
// complete and concurrent launchers end up sharing one. Nothing in the app home
// ~/.wipnote may key a store.
func CreateExternalStore(dir, origin string, seed func(storeDir string) error) (root string, created bool, err error) {
	key := realDir(dir)
	if s, ok := scopeOf(dir); ok {
		key = s.MainRoot
	}
	if rel, rerr := filepath.Rel(realDir(DataDir()), key); rerr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, fmt.Errorf("%s is inside wipnote's own home (%s), which holds stores and is never a project; run from a project folder", dir, DataDir())
	}
	root = ShadowRootFor(key)
	if validStoreOwner(root) {
		return root, false, nil
	}
	if isDir(root) {
		return "", false, fmt.Errorf("store %s exists but is not usable: it has no .wipnote/ or no valid %s; remove it to start over", root, ProjectFileName)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return "", false, err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(root), ".tmp-"+filepath.Base(root)+"-")
	if err != nil {
		return "", false, err
	}
	defer os.RemoveAll(tmp) //nolint:errcheck // nothing is left after the rename
	if err := buildShadowRoot(tmp, projectFile{Kind: externalStoreKind, CodeRoot: key, Origin: origin}, seed); err != nil {
		return "", false, err
	}
	if err := os.Rename(tmp, root); err != nil {
		// Lost a race: to another launcher here, or to a project encoding alike.
		if again := ShadowRootFor(key); again == root && validStoreOwner(root) {
			return root, false, nil
		} else if again != root && os.Rename(tmp, again) == nil {
			return again, true, nil
		}
		return "", false, err
	}
	return root, true, nil
}

func buildShadowRoot(root string, pf projectFile, seed func(string) error) error {
	storeDir := filepath.Join(root, ".wipnote")
	if err := os.MkdirAll(storeDir, 0o755); err != nil {
		return err
	}
	if err := writeProjectFile(root, pf); err != nil {
		return err
	}
	if seed != nil {
		if err := seed(storeDir); err != nil {
			return err
		}
	}
	return initStoreGit(root)
}

func writeProjectFile(root string, pf projectFile) error {
	data, err := json.MarshalIndent(pf, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, ProjectFileName), append(data, '\n'), 0o644)
}

// MarkStoreExplicit records that the shadow root owner was asked for by `wipnote
// init`, so walk-ups from folders below its code root honour it, and commits the
// change to the store's repo. It reports whether anything changed.
func MarkStoreExplicit(owner string) (bool, error) {
	pf, ok := readProjectFile(owner)
	if !ok || pf.Origin == OriginInit {
		return false, nil
	}
	pf.Origin = OriginInit
	if err := writeProjectFile(owner, pf); err != nil {
		return false, err
	}
	if _, err := gitOutput(owner, "add", "--", ProjectFileName); err != nil {
		return true, err
	}
	_, err := gitOutput(owner, "commit", "--quiet", "--no-verify", "-m", "Mark store as made by wipnote init", "--", ProjectFileName)
	return true, err
}

// initStoreGit makes root a git repo that can always commit: no remote, no
// template or user hooks, no signing prompt, and a local identity only when
// git has none. The first commit holds what the store starts with.
func initStoreGit(root string) error {
	if _, err := gitOutput(root, "init", "--quiet", "--template="); err != nil {
		return err
	}
	settings := [][]string{{"commit.gpgsign", "false"}, {"core.hooksPath", os.DevNull}}
	for _, key := range []string{"user.name", "user.email"} {
		if v, err := gitOutput(root, "config", "--get", key); err != nil || v == "" {
			settings = append(settings, []string{key, map[string]string{"user.name": "wipnote", "user.email": "wipnote@localhost"}[key]})
		}
	}
	for _, kv := range settings {
		if _, err := gitOutput(root, "config", "--local", kv[0], kv[1]); err != nil {
			return err
		}
	}
	if _, err := gitOutput(root, "add", "-A"); err != nil {
		return err
	}
	_, err := gitOutput(root, "commit", "--quiet", "--no-verify", "-m", "Create wipnote store")
	return err
}
