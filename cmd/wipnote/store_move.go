package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/spf13/cobra"
)

// untrackStoreMessage is the one commit `store move --untrack` makes in the
// project's own repo.
const untrackStoreMessage = "chore: stop tracking the wipnote store (moved to the app store)"

func storeMoveCmd() *cobra.Command {
	var dryRun, untrack bool
	cmd := &cobra.Command{
		Use:   "move",
		Short: "Move this project's in-repo .wipnote/ store to the app store",
		Long: `Copies the project's .wipnote/ store into the app store under
~/.wipnote/projects and makes wipnote use the copy from then on. After the move
wipnote commits work-item changes to the store's own git repository, so they no
longer land on the project's branches.

Run it from the project. By default nothing in the project changes: the old
folder stays where it is and wipnote ignores it, also when an old commit puts
it back. Runtime files (locks, pids, sockets) are not copied.

With --untrack it also removes .wipnote/ from the project's git index in one
commit that touches no other path, and then deletes the folder.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStoreMove(cmd.OutOrStdout(), dryRun, untrack)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would happen and change nothing")
	cmd.Flags().BoolVar(&untrack, "untrack", false, "also commit the removal of .wipnote/ from the project's git index and delete the folder")
	return cmd
}

func runStoreMove(w io.Writer, dryRun, untrack bool) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("store move: %w", err)
	}
	// The project is where the command runs: an environment override must not
	// send a move to another project's store.
	code := paths.MainRepoRoot(cwd)
	if code == "" {
		code = cwd
	}
	src := filepath.Join(code, ".wipnote")
	owner := paths.StoreOwnerFor(code)
	moved := paths.IsMovedStore(owner)
	switch {
	case moved:
	case paths.IsExternalStore(owner):
		return fmt.Errorf("store move: this project's store is already in the app store at %s", owner)
	case owner != code:
		return fmt.Errorf("store move: %s has no .wipnote/ store to move", code)
	}
	if dryRun {
		return printStoreMovePlan(w, code, src, moved, untrack)
	}
	if !moved {
		if owner, err = moveStore(w, code, src); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "The store was already moved to %s.\n", owner)
	}
	if !untrack {
		printUntrackAdvice(w, code, src)
		return nil
	}
	return untrackInRepoStore(w, code, src)
}

func printStoreMovePlan(w io.Writer, code, src string, moved, untrack bool) error {
	dest := paths.ShadowRootFor(code)
	if moved {
		fmt.Fprintf(w, "Dry run: the store was already moved to %s.\n", dest)
	} else {
		files, err := storeMoveFiles(src)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Dry run: would copy %d file(s) from %s into %s.\n", len(files), src, filepath.Join(dest, ".wipnote"))
	}
	if untrack {
		fmt.Fprintf(w, "Dry run: would commit the removal of %d tracked file(s) and delete %s.\n", len(trackedStoreFiles(code)), src)
	}
	fmt.Fprintln(w, "Nothing was changed.")
	return nil
}

// moveStore copies the in-repo store into a new shadow root and returns it.
func moveStore(w io.Writer, code, src string) (string, error) {
	// A queued commit names a path in the project's repo. After the move that
	// path is stale, so the queue must be empty first.
	if outbox, err := openCommitOutbox(code); err == nil {
		if n, derr := outbox.Depth(); derr == nil && n > 0 {
			return "", fmt.Errorf("store move: %d store commit(s) are still queued for this repo; run `wipnote commit-queue flush` first", n)
		}
	}
	files, err := storeMoveFiles(src)
	if err != nil {
		return "", err
	}
	root, err := paths.MoveStoreToExternal(code, func(storeDir string) error {
		for _, rel := range files {
			if _, cerr := copyNewFile(filepath.Join(src, rel), filepath.Join(storeDir, rel)); cerr != nil {
				return fmt.Errorf("copy %s: %w", rel, cerr)
			}
		}
		return seedStore(storeDir)
	})
	if err != nil {
		return "", fmt.Errorf("store move: %w", err)
	}
	fmt.Fprintf(w, "Copied %d file(s) into %s.\nwipnote now uses that store for %s.\n", len(files), filepath.Join(root, ".wipnote"), code)
	return root, nil
}

// storeMoveFiles lists the files under src to copy, relative to src.
func storeMoveFiles(src string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || skipRuntimeEntry(d) {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("store move: read %s: %w", src, err)
	}
	return files, nil
}

// trackedStoreFiles lists the .wipnote/ paths in the project's git index.
func trackedStoreFiles(code string) []string {
	if !isGitRepo(code) {
		return nil
	}
	out, err := gitRunner(code, "ls-files", "--", ".wipnote")
	if err != nil {
		return nil
	}
	return strings.Fields(strings.TrimSpace(string(out)))
}

func printUntrackAdvice(w io.Writer, code, src string) {
	if _, err := os.Stat(src); err != nil {
		return
	}
	fmt.Fprintf(w, "%s was left as it is and wipnote no longer reads it.\n", src)
	if len(trackedStoreFiles(code)) > 0 {
		fmt.Fprintln(w, "Run `wipnote store move --untrack` to remove it from the project's git index in one commit and delete it.")
		return
	}
	fmt.Fprintln(w, "Delete it when you no longer need it.")
}

// untrackInRepoStore removes the tracked .wipnote/ files in one commit limited
// to that path, then deletes what is left of the folder. `git commit -- <path>`
// records the working tree's state of the path, so the files must leave the
// working tree with the index. The app store already holds a copy of each.
func untrackInRepoStore(w io.Writer, code, src string) error {
	if n := len(trackedStoreFiles(code)); n > 0 {
		out, err := runGitMutationBatch(code,
			[]string{"rm", "-r", "-q", "-f", "--", ".wipnote"},
			[]string{"commit", "-q", "-m", untrackStoreMessage, "--", ".wipnote"})
		if err != nil {
			return fmt.Errorf("store move: untrack %s: %v: %s (the app store keeps a copy; `git reset -q -- .wipnote && git checkout -- .wipnote` puts the tracked files back)", src, err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(w, "Committed the removal of %d tracked file(s) from the project's git index.\n", n)
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("store move: delete %s: %w", src, err)
	}
	fmt.Fprintf(w, "Deleted %s.\n", src)
	return nil
}
