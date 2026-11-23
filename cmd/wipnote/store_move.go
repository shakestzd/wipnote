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

// storeMovePlan says what a move from the current folder would do.
type storeMovePlan struct {
	cwd   string
	code  string // the project: a repo's main checkout, or the folder that holds the store
	src   string // the in-tree store folder to copy; "" once the store is moved
	rel   string // the store folder relative to a checkout's top
	owner string // the moved store's shadow root; "" before the move
	inGit bool
	note  string // something the owner should know, or ""
}

// planStoreMove finds the in-tree store that serves the working directory. An
// environment override must not send a move to another project's store, so
// only the working directory counts. ok is false when there is nothing to
// move, and why says so.
func planStoreMove() (p storeMovePlan, why string, ok bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return p, err.Error(), false
	}
	p.cwd, p.code = cwd, paths.MainRepoRoot(cwd)
	p.inGit = p.code != ""
	owner := paths.StoreOwnerFor(cwd)
	switch {
	case paths.IsMovedStore(owner):
		p.owner, p.rel = owner, paths.MovedFrom(owner)
		if !p.inGit {
			p.code = paths.CodeRoot(owner)
		}
		return p, "", true
	case paths.IsExternalStore(owner):
		return p, "this project's store is already in the app store at " + owner, false
	case owner == "":
		return p, cwd + " has no .wipnote/ store to move", false
	case !p.inGit:
		p.code, p.src, p.rel = owner, filepath.Join(owner, ".wipnote"), ".wipnote"
		return p, "", true
	}
	// In a repo the main checkout's root store is the project's store. A linked
	// worktree holds a copy of it, and a store below the root is a second one.
	if _, serr := os.Stat(filepath.Join(p.code, ".wipnote")); serr == nil {
		p.src, p.rel = filepath.Join(p.code, ".wipnote"), ".wipnote"
		if top := checkoutTop(owner); owner != p.code && owner != top {
			p.note = fmt.Sprintf("%s holds a second store. wipnote moves the store at the repo's root and no longer reads the second one. Copy what you need from it by hand.", filepath.Join(owner, ".wipnote"))
		}
		return p, "", true
	}
	rel, rerr := filepath.Rel(checkoutTop(owner), owner)
	if rerr != nil || strings.HasPrefix(rel, "..") {
		return p, "cannot place " + owner + " inside its checkout", false
	}
	p.src, p.rel = filepath.Join(owner, ".wipnote"), filepath.Join(rel, ".wipnote")
	return p, "", true
}

// checkoutTop is the top of the git checkout that holds dir, or dir.
func checkoutTop(dir string) string {
	out, err := gitRunner(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return dir
	}
	if top := strings.TrimSpace(string(out)); top != "" {
		return top
	}
	return dir
}

func runStoreMove(w io.Writer, dryRun, untrack bool) error {
	p, why, ok := planStoreMove()
	if !ok {
		return fmt.Errorf("store move: %s", why)
	}
	if dryRun {
		return printStoreMovePlan(w, p, untrack)
	}
	if p.note != "" {
		fmt.Fprintln(w, p.note)
	}
	if p.owner == "" {
		var err error
		if p.owner, err = moveStore(w, p); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "The store was already moved to %s.\n", p.owner)
	}
	if !untrack {
		printUntrackAdvice(w, p)
		return nil
	}
	return removeOldStore(w, p, func(top, reason string) error {
		return fmt.Errorf("store move: %s: %s; run `wipnote store move --untrack` again afterwards", top, reason)
	})
}

// removeOldStore takes the old store folder out of the project: out of the
// index and the working tree of each checkout in a repo, off the disk
// elsewhere. busy is called for a checkout that cannot take a commit now.
func removeOldStore(w io.Writer, p storeMovePlan, busy func(top, reason string) error) error {
	if !p.inGit {
		return deleteOldStoreFolder(w, filepath.Join(p.code, p.rel))
	}
	tops := []string{p.code}
	if top := checkoutTop(p.cwd); top != p.code {
		tops = append(tops, top)
	}
	for _, top := range tops {
		if _, serr := os.Stat(filepath.Join(top, p.rel)); serr != nil && len(trackedStoreFiles(top, p.rel)) == 0 {
			continue
		}
		if reason := checkoutBusy(top); reason != "" {
			if err := busy(top, reason); err != nil {
				return err
			}
			continue
		}
		if err := untrackInRepoStore(w, top, p.rel); err != nil {
			return err
		}
	}
	return nil
}

func printStoreMovePlan(w io.Writer, p storeMovePlan, untrack bool) error {
	if p.note != "" {
		fmt.Fprintln(w, p.note)
	}
	old := filepath.Join(p.code, p.rel)
	if p.owner != "" {
		fmt.Fprintf(w, "Dry run: the store was already moved to %s.\n", p.owner)
	} else {
		old = p.src
		files, err := storeMoveFiles(p.src)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Dry run: would copy %d file(s) from %s into %s.\n", len(files), p.src, filepath.Join(paths.ShadowRootFor(p.code), ".wipnote"))
	}
	switch {
	case untrack && p.inGit:
		fmt.Fprintf(w, "Dry run: would commit the removal of %d tracked file(s) and delete %s.\n", len(trackedStoreFiles(p.code, p.rel)), old)
	case untrack:
		fmt.Fprintf(w, "Dry run: would delete %s.\n", old)
	}
	fmt.Fprintln(w, "Nothing was changed.")
	return nil
}

// moveStore copies the in-repo store into a new shadow root and returns it.
func moveStore(w io.Writer, p storeMovePlan) (string, error) {
	code, src := p.code, p.src
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
	root, err := paths.MoveStoreToExternal(code, p.rel, func(storeDir string) error {
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

// trackedStoreFiles lists the paths under rel in the index of the checkout at
// top.
func trackedStoreFiles(top, rel string) []string {
	if !isGitRepo(top) {
		return nil
	}
	out, err := gitRunner(top, "ls-files", "-z", "--", rel)
	if err != nil {
		return nil
	}
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == 0 })
}

func printUntrackAdvice(w io.Writer, p storeMovePlan) {
	old := filepath.Join(p.code, p.rel)
	if _, err := os.Stat(old); err != nil {
		return
	}
	fmt.Fprintf(w, "%s was left as it is and wipnote no longer reads it.\n", old)
	if len(trackedStoreFiles(p.code, p.rel)) > 0 {
		fmt.Fprintln(w, "Run `wipnote store move --untrack` to remove it from the project's git index in one commit and delete it.")
		return
	}
	fmt.Fprintln(w, "Run `wipnote store move --untrack` to delete it.")
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
func untrackInRepoStore(w io.Writer, top, rel string) error {
	src := filepath.Join(top, rel)
	if n := len(trackedStoreFiles(top, rel)); n > 0 {
		out, err := runGitMutationBatch(top,
			[]string{"rm", "-r", "-q", "-f", "--", rel},
			[]string{"commit", "-q", "--no-verify", "-m", untrackStoreMessage, "--", rel})
		if err != nil {
			return fmt.Errorf("store move: untrack %s: %v: %s (the app store keeps a copy; `git reset -q -- %s && git checkout -- %s` puts the tracked files back)", src, err, strings.TrimSpace(string(out)), rel, rel)
		}
		fmt.Fprintf(w, "Committed the removal of %d tracked wipnote store file(s) from %s.\n", n, top)
	}
	excludeStoreFolder(top, rel)
	return deleteOldStoreFolder(w, src)
}

// deleteOldStoreFolder deletes what is left of a moved store's old folder. The
// app store holds a copy of every record in it.
func deleteOldStoreFolder(w io.Writer, src string) error {
	if _, err := os.Stat(src); err != nil {
		return nil
	}
	if err := os.RemoveAll(src); err != nil {
		// A wipnote process started before the move can still write files here.
		fmt.Fprintf(w, "Could not delete all of %s (%v). wipnote no longer reads it. A wipnote session started before the move still writes there. Exit that session, then delete the folder.\n", src, err)
		return nil
	}
	fmt.Fprintf(w, "Deleted %s.\n", src)
	return nil
}

// excludeStoreFolder keeps a stray .wipnote/ out of the repo's `git status`. A
// wipnote process started before the move can put runtime files back there.
// The rule goes in the repo's own info/exclude, which git never commits.
func excludeStoreFolder(top, rel string) {
	line := "/" + filepath.ToSlash(rel) + "/"
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

// autoMoveStore takes a project's in-tree store out of the project
// (feat-b153fe64). Every wipnote command copies the store to the app store the
// first time it runs in such a project, so no command writes to the project's
// branches again. A launcher (removeOld) also untracks the old folder in the
// main checkout and in the checkout it runs in, and deletes it. A branch that
// still tracks the folder is untracked the first time a launch finds it
// checked out. Nothing here may stop the command, so every problem is a
// notice on w.
func autoMoveStore(w io.Writer, removeOld bool) {
	p, _, ok := planStoreMove()
	if !ok {
		return
	}
	if p.owner == "" {
		fmt.Fprintln(w, "wipnote: moving this project's store to the app store, so wipnote no longer commits to the project's branches.")
		if p.note != "" {
			fmt.Fprintf(w, "wipnote: %s\n", p.note)
		}
		owner, err := moveStore(w, p)
		if err != nil {
			fmt.Fprintf(w, "wipnote: %v\n", err)
			return
		}
		p.owner = owner
		if !removeOld {
			fmt.Fprintf(w, "wipnote: the next `wipnote claude`, `codex`, `antigravity` or `yolo` launch removes %s from the project, or run `wipnote store move --untrack`.\n", filepath.Join(p.code, p.rel))
		}
	}
	if !removeOld {
		return
	}
	err := removeOldStore(w, p, func(top, reason string) error {
		fmt.Fprintf(w, "wipnote: %s still holds %s and %s, so it stays for now. A later launch removes it.\n", top, p.rel, reason)
		return nil
	})
	if err != nil {
		fmt.Fprintf(w, "wipnote: %v\n", err)
	}
}

// storeMoveOnAnyCommand: an ordinary command that names no project of its own.
// A command told which project to use (--project-dir, WIPNOTE_PROJECT_DIR)
// may not be running in that project's folder, and `store` and `init` decide
// about the store themselves.
func storeMoveOnAnyCommand(cmd *cobra.Command) bool {
	if projectDirFlag != "" || os.Getenv("WIPNOTE_PROJECT_DIR") != "" || isHarnessLauncherCmd(cmd) {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "store", "init", "help", "version", "completion", "build", "upgrade", "update", "doctor":
			return false
		}
	}
	return true
}
