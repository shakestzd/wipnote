package session

import (
	"errors"
	"fmt"
	"github.com/shakestzd/wipnote/core/paths"
	"io"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/worktree"
)

// Parallel worktree workers (feat-4310a889): spawn --worktree gives a worker
// its own checkout at <root>/.claude/worktrees/<name> on a new branch
// delegate/<name>, so several workers can edit one item's files at once.

// ErrBadWorktree is every --worktree refusal. Test it with errors.Is.
var ErrBadWorktree = errors.New("delegate: cannot use --worktree")

// worktreeLock serialises worktree creation across spawn processes: git
// takes its own locks, but two adds at once can still fail on the shared
// config. Held for the add only.
const worktreeLock = "worktrees.lock"

// wtInfo is a worktree worker's checkout: path, branch and the commit the
// branch was cut from (Info.Worktree, Branch, BaseSHA).
type wtInfo struct{ Path, Branch, BaseSHA string }

func (w wtInfo) set() bool { return w.Path != "" }

// parallel reports a worker that has its own checkout: it may share its
// work item with another live worker.
func (o SpawnOptions) parallel() bool { return o.Worktree || o.wt.set() }

// WorktreePath and WorktreeBranch name a worker's checkout.
func WorktreePath(root, name string) string {
	return filepath.Join(root, ".claude", "worktrees", name)
}
func WorktreeBranch(name string) string { return "delegate/" + name }

func refuseWorktree(why string, a ...any) error {
	return fmt.Errorf("%w: "+why, append([]any{ErrBadWorktree}, a...)...)
}

// checkWorktree is spawn's --worktree refusals, before anything is made.
func checkWorktree(o SpawnOptions) error {
	switch {
	case !o.Worktree:
		return nil
	case o.Container:
		return refuseWorktree("it cannot be combined with --container; a devcontainer worker runs in the container's own folder, not a host checkout")
	case o.ResumedFrom != "":
		return refuseWorktree("it cannot be combined with --resume; a resume runs in the folder the old worker ran in")
	case o.Harness != HarnessClaude:
		return refuseWorktree("it is for Claude workers; the Codex launcher cannot run in a folder apart from the project root")
	case o.Root == "":
		return refuseWorktree("the project root is unknown")
	}
	return nil
}

// createWorktree makes the worker's checkout under the worktree lock and
// points the spawn at it. Nothing else exists yet, so a failure leaves
// nothing behind.
func createWorktree(d Deps, o *SpawnOptions) error {
	if err := os.MkdirAll(d.Store.Root(), 0o700); err != nil {
		return fmt.Errorf("delegate: run store: %w", err)
	}
	release := filelock.Guard(filepath.Join(d.Store.Root(), worktreeLock))
	defer release()
	base := o.Base
	if base == "" {
		base = "HEAD"
	}
	code := paths.CodeRoot(o.Root) // the checkout belongs to the source repo, the run to the store
	path, branch := WorktreePath(code, o.Name), WorktreeBranch(o.Name)
	sha, err := worktree.CreateOnNewBranch(code, path, branch, base, io.Discard)
	if err != nil {
		return refuseWorktree("%v", err)
	}
	o.wt = wtInfo{Path: path, Branch: branch, BaseSHA: sha}
	o.Cwd, o.WorkDir, o.ProjectDir = path, path, o.Root
	return nil
}

// dropWorktree removes a worktree this spawn just made, when the spawn
// failed after making it. It is untouched, so it is clean.
func dropWorktree(o SpawnOptions) {
	if o.Worktree && o.wt.set() {
		worktree.RemoveIfClean(paths.CodeRoot(o.Root), o.wt.Path, o.wt.Branch, o.wt.BaseSHA)
	}
}

// linkedDirs sets WorkDir and ProjectDir for a Claude worker spawned from a
// folder other than the wipnote root (a linked worktree): it runs where its
// caller runs, while hooks and the ledger stay on the root.
func linkedDirs(o *SpawnOptions) {
	if o.WorkDir != "" || o.Root == "" || o.Harness != HarnessClaude || o.Container || o.ResumedFrom != "" {
		return
	}
	if cwd, root := realPath(o.Cwd), realPath(o.Root); o.Cwd != "" && cwd != root {
		o.WorkDir, o.ProjectDir = filepath.Clean(o.Cwd), o.Root
	}
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// seedTrust pre-seeds workspace trust for the worker's folder (Deps.PreseedTrust),
// so no dialog blocks its first prompt. A failure is returned as a warning.
func seedTrust(d Deps, o SpawnOptions) string {
	if d.PreseedTrust == nil || o.WorkDir == "" || o.Harness != HarnessClaude {
		return ""
	}
	if err := d.PreseedTrust(o.WorkDir); err != nil {
		return "workspace trust was not pre-seeded: " + err.Error()
	}
	return ""
}

// settleWorktree runs after a stop closed the worker: it removes the
// worktree and branch when untouched, else keeps them and says where.
func settleWorktree(res *StopResult, info Info) {
	if info.Worktree == "" {
		return
	}
	res.Worktree, res.Branch = info.Worktree, info.Branch
	if len(res.Survivors) > 0 {
		res.Kept = true
		return
	}
	if kept, _ := worktree.RemoveIfClean(paths.CodeRoot(info.ProjectDir), info.Worktree, info.Branch, info.BaseSHA); kept {
		res.Kept = true
		return
	}
	res.WorktreeRemoved = true
}
