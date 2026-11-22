package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/commitqueue"
	"github.com/spf13/cobra"
)

// untrackStoreMessage is the one commit wipnote makes in a project's own repo
// when its store leaves that repo.
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

The launchers (wipnote claude, codex, antigravity, yolo) do this on their own,
with --untrack. Run the command yourself to move a project you do not launch.

Without --untrack nothing in the project changes: the old folder stays where it
is and wipnote ignores it, also when an old commit puts it back. Runtime files
(locks, pids, sockets) are not copied.

With --untrack it also removes .wipnote/ from the project's git index in one
commit that touches no other path and runs no commit hook, then deletes the
folder.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runStoreMove(cmd.OutOrStdout(), dryRun, untrack)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would happen and change nothing")
	cmd.Flags().BoolVar(&untrack, "untrack", false, "also commit the removal of .wipnote/ from the project's git index and delete the folder")
	return cmd
}

// storeMoveProject is the project the command runs in: a repo's main checkout,
// or the current folder outside a repo. An environment override must not send
// a move to another project's store, so only the working directory counts.
func storeMoveProject() (cwd, code string, err error) {
	if cwd, err = os.Getwd(); err != nil {
		return "", "", fmt.Errorf("store move: %w", err)
	}
	if code = paths.MainRepoRoot(cwd); code == "" {
		code = cwd
	}
	return cwd, code, nil
}

func runStoreMove(w io.Writer, dryRun, untrack bool) error {
	_, code, err := storeMoveProject()
	if err != nil {
		return err
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
	if reason := checkoutBusy(code); reason != "" {
		return fmt.Errorf("store move: %s: %s; run `wipnote store move --untrack` again afterwards", code, reason)
	}
	return untrackInRepoStore(w, code)
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
	// path is stale, so the queue is drained into the project one last time.
	if outbox, err := openCommitOutbox(code); err == nil {
		_, _ = outbox.Flush(outboxCommitter, commitqueue.MaxAttempts)
		if n, derr := outbox.Depth(); derr == nil && n > 0 {
			return "", fmt.Errorf("store move: %d store commit(s) are still queued for this repo and `wipnote commit-queue flush` cannot drain them; resolve them first", n)
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

// trackedStoreFiles lists the .wipnote/ paths in the index of the checkout at
// top.
func trackedStoreFiles(top string) []string {
	if !isGitRepo(top) {
		return nil
	}
	out, err := gitRunner(top, "ls-files", "--", ".wipnote")
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

// checkoutBusy says why the checkout at top cannot take a commit right now, or
// "". A commit made on a detached HEAD or in the middle of a merge, rebase,
// cherry-pick, revert or bisect would land in the wrong place.
func checkoutBusy(top string) string {
	if _, err := gitRunner(top, "symbolic-ref", "-q", "HEAD"); err != nil {
		return "HEAD is detached"
	}
	for _, name := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "BISECT_LOG", "rebase-merge", "rebase-apply"} {
		out, err := gitRunner(top, "rev-parse", "--git-path", name)
		if err != nil {
			continue
		}
		p := strings.TrimSpace(string(out))
		if !filepath.IsAbs(p) {
			p = filepath.Join(top, p)
		}
		if _, serr := os.Stat(p); serr == nil {
			return "a merge, rebase or similar git operation is in progress"
		}
	}
	return ""
}

// untrackInRepoStore removes the tracked .wipnote/ files of the checkout at
// top in one commit limited to that path, then deletes what is left of the
// folder. `git commit -- <path>` records the working tree's state of the path,
// so the files must leave the working tree with the index. The app store
// already holds a copy of each. The commit runs no hook: it only deletes
// store files, and a project's commit checks have nothing to say about it.
func untrackInRepoStore(w io.Writer, top string) error {
	src := filepath.Join(top, ".wipnote")
	if n := len(trackedStoreFiles(top)); n > 0 {
		out, err := runGitMutationBatch(top,
			[]string{"rm", "-r", "-q", "-f", "--", ".wipnote"},
			[]string{"commit", "-q", "--no-verify", "-m", untrackStoreMessage, "--", ".wipnote"})
		if err != nil {
			return fmt.Errorf("store move: untrack %s: %v: %s (the app store keeps a copy; `git reset -q -- .wipnote && git checkout -- .wipnote` puts the tracked files back)", src, err, strings.TrimSpace(string(out)))
		}
		fmt.Fprintf(w, "Committed the removal of %d tracked wipnote store file(s) from %s.\n", n, top)
	}
	excludeStoreFolder(top)
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.RemoveAll(src); err != nil {
		// A wipnote process started before the move can still hold files here.
		fmt.Fprintf(w, "Could not delete all of %s (%v). It is no longer read; delete it once older wipnote processes have stopped.\n", src, err)
		return nil
	}
	fmt.Fprintf(w, "Deleted %s.\n", src)
	return nil
}

// excludeStoreFolder keeps a stray .wipnote/ out of the repo's `git status`. A
// wipnote process started before the move can put runtime files back there.
// The rule goes in the repo's own info/exclude, which git never commits.
func excludeStoreFolder(top string) {
	const line = "/.wipnote/"
	out, err := gitRunner(top, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	commonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(top, commonDir)
	}
	path := filepath.Join(commonDir, "info", "exclude")
	existing, _ := os.ReadFile(path)
	for _, l := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(l) == line {
			return
		}
	}
	if os.MkdirAll(filepath.Dir(path), 0o755) != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n" + line + "\n")
}

// autoMoveLauncherStore takes a project's in-repo store out of its git when a
// launcher starts there (feat-b153fe64): it moves the store to the app store,
// then untracks .wipnote/ in the main checkout and in the checkout the launch
// runs in. Each branch that still tracks the folder is untracked the first
// time a launch finds it checked out. Nothing here may stop the launch, so
// every problem is a notice on w.
func autoMoveLauncherStore(w io.Writer) {
	cwd, code, err := storeMoveProject()
	if err != nil {
		return
	}
	owner := paths.StoreOwnerFor(code)
	if !paths.IsMovedStore(owner) {
		if owner != code || paths.IsExternalStore(owner) {
			return // no store yet, an app store already, or a store nested below the repo root
		}
		fmt.Fprintf(w, "wipnote: moving this project's store out of its git repository, so wipnote no longer commits to the project's branches.\n")
		if _, err := moveStore(w, code, filepath.Join(code, ".wipnote")); err != nil {
			fmt.Fprintf(w, "wipnote: %v\n", err)
			return
		}
	}
	if !isGitRepo(code) {
		return // nothing is tracked outside a repo; the old folder is left alone
	}
	tops := []string{code}
	if top, terr := gitRunner(cwd, "rev-parse", "--show-toplevel"); terr == nil {
		if t := strings.TrimSpace(string(top)); t != "" && t != code {
			tops = append(tops, t)
		}
	}
	for _, top := range tops {
		if _, serr := os.Stat(filepath.Join(top, ".wipnote")); serr != nil && len(trackedStoreFiles(top)) == 0 {
			continue
		}
		if reason := checkoutBusy(top); reason != "" {
			fmt.Fprintf(w, "wipnote: %s still tracks .wipnote/ and %s, so it stays tracked for now. A later launch removes it.\n", top, reason)
			continue
		}
		if err := untrackInRepoStore(w, top); err != nil {
			fmt.Fprintf(w, "wipnote: %v\n", err)
		}
	}
}
