package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/worktree"
	"github.com/shakestzd/wipnote/internal/registry"
	"github.com/spf13/cobra"
)

// errContainerExternalStore: a devcontainer mounts the repo, and an app-owned
// store is outside it.
var errContainerExternalStore = errors.New("delegate --container cannot be used with an external store: the container cannot reach a store kept outside the repo; run the worker on the host, or use an in-repo store (`wipnote init --in-repo`)")

// withoutHomeEntry drops a registry entry for $HOME or the app home. Older
// versions registered $HOME as a project; ~/.wipnote is the application's home.
// The file keeps the entry.
func withoutHomeEntry(entries []registry.Entry) []registry.Entry {
	kept := make([]registry.Entry, 0, len(entries))
	for _, e := range entries {
		if !paths.IsReservedDir(e.ProjectDir) {
			kept = append(kept, e)
		}
	}
	return kept
}

// codeRootOf is the source repo of the store at wipnoteDir.
func codeRootOf(wipnoteDir string) string {
	return paths.CodeRoot(filepath.Dir(wipnoteDir))
}

// externalWipnoteRoot fills an unset wipnote root from an external store, so
// every launch mode gets the two roots from the exec chokepoints, resume and
// continue paths included. Any other value is returned as given.
func externalWipnoteRoot(wipnoteRoot, projectRoot string) string {
	if wipnoteRoot != "" || projectRoot == "" {
		return wipnoteRoot
	}
	return paths.ExternalStoreFor(projectRoot)
}

// launchStateRoot is where launcher state that lives in .wipnote/ belongs (the
// OTel collector, the guard profile, first-launch notices). It is the shadow
// root under an external store and otherwise projectRoot, as it always was.
func launchStateRoot(wipnoteRoot, projectRoot string) string {
	if paths.IsExternalStore(wipnoteRoot) {
		return wipnoteRoot
	}
	return projectRoot
}

// ensureExternalStore makes the app-owned store for dir and says where it is.
// origin records who asked (paths.OriginInit or paths.OriginLauncher). It does
// nothing when a store already serves dir, and returns an error only when the
// store could not be made.
func ensureExternalStore(dir, origin string) (string, error) {
	if owner := paths.StoreOwnerFor(dir); owner != "" {
		return "", nil
	}
	root, created, err := paths.CreateExternalStore(dir, origin, seedStore)
	if err != nil {
		return "", err
	}
	if created {
		fmt.Fprintf(os.Stderr, "wipnote: this project's store is at %s (nothing is added to the project folder)\n", root)
	}
	return root, nil
}

// autoStore gives the working folder's project its app-owned store when no
// store serves it, the way a launcher does: a git repo's is keyed by its main
// root, so linked worktrees and submodules share it, and a plain folder's (a
// repo rooted at $HOME counts as none; $HOME itself is a plain folder) by the
// folder. Nothing is ever added to the project. The store counts as a
// launcher's, so walk-ups from plain folders below never adopt it. The app home
// ~/.wipnote gets none. It returns "" when the folder may not have one or it
// could not be made (that error is printed).
func autoStore() string {
	cwd, err := os.Getwd()
	if err != nil || paths.InAppHome(cwd) {
		return ""
	}
	root, err := ensureExternalStore(cwd, paths.OriginLauncher)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wipnote: could not make this project's store: %v\n", err)
		return ""
	}
	if root == "" { // already there; ensureExternalStore makes nothing then
		root = paths.StoreOwnerFor(cwd)
	}
	return root
}

// seedStore lays out a new store the way `wipnote init` does.
func seedStore(storeDir string) error {
	if err := createSubdirs(storeDir); err != nil {
		return err
	}
	if err := writeStylesCSS(storeDir); err != nil {
		return err
	}
	worktree.EnsureWipnoteGitignore(filepath.Dir(storeDir))
	return nil
}

// ensureLauncherStore gives the project the launcher runs in a store when it has
// none. A value in the environment that already names a store takes priority.
func ensureLauncherStore() error {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}
	root, err := paths.ResolveProjectDir(paths.ProjectDirOptions{})
	if err != nil || paths.IsStoreOwner(root) {
		return nil
	}
	if _, err = ensureExternalStore(cwd, paths.OriginLauncher); err != nil {
		return fmt.Errorf("create store: %w", err)
	}
	return nil
}

// launcherMayCreateStore: a real launch of a top-level harness command. Dry
// runs, cleanup and install modes, explicit project flags and delegate workers
// (who get --work-dir and a known store) all leave things as they are.
func launcherMayCreateStore(cmd *cobra.Command) bool {
	if !isHarnessLauncherCmd(cmd) || cmd.Parent() == nil || cmd.Parent().Parent() != nil || projectDirFlag != "" {
		return false
	}
	refusing := []string{"dry-run", "cleanup", "work-dir"}
	if name := cmd.Name(); name != "claude" && name != "yolo" { // their --init launches; the others only install
		refusing = append(refusing, "init")
	}
	for _, name := range refusing {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return false
		}
	}
	return true
}
