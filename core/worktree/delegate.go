package worktree

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// git runs git -C dir and returns its trimmed output.
func git(dir string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// CreateOnNewBranch adds a worktree at path on the NEW branch branch, cut
// from base, and returns base's commit id. Unlike the idempotent Ensure*
// helpers it never reuses: a taken path or branch is refused, and a failed
// add leaves no folder (a delegate worker owns its checkout). .wipnote/ is
// excluded from the new worktree as for the other worktrees.
func CreateOnNewBranch(repoRoot, path, branch, base string, w io.Writer) (string, error) {
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("worktree folder %s already exists", path)
	}
	if _, err := git(repoRoot, "rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil {
		return "", fmt.Errorf("branch %s already exists", branch)
	}
	sha, err := git(repoRoot, "rev-parse", "--verify", "-q", base+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("base %q is not a commit in %s", base, repoRoot)
	}
	_ = os.MkdirAll(parentOf(path), 0o755)
	ExcludeWorktreesDir(repoRoot)
	if out, err := git(repoRoot, "worktree", "add", "-q", "-b", branch, path, sha); err != nil {
		return "", fmt.Errorf("git worktree add failed: %w\n%s", err, out)
	}
	excludeWipnoteFromWorktree(path, w)
	return sha, nil
}

func parentOf(path string) string {
	if i := strings.LastIndexByte(path, os.PathSeparator); i > 0 {
		return path[:i]
	}
	return "."
}

// RemoveIfClean removes the worktree and its branch only when the worktree
// has no changes, no untracked files, its HEAD still at baseSHA and the branch
// no commit beyond baseSHA. Otherwise it removes nothing and returns kept with the reason.
// It never forces: git worktree remove without --force refuses a dirty
// tree, and the branch is deleted with a compare against its own tip.
// A worktree folder that is already gone is not kept.
func RemoveIfClean(repoRoot, path, branch, baseSHA string) (kept bool, why string) {
	if _, err := os.Stat(path); err != nil {
		_ = exec.Command("git", "-C", repoRoot, "worktree", "prune").Run()
		return false, ""
	}
	if out, err := git(path, "status", "--porcelain", "--untracked-files=all"); err != nil || out != "" {
		return true, "the worktree has changes or untracked files"
	}
	tip, err := git(repoRoot, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if err != nil || tip != baseSHA {
		return true, "the branch has commits beyond its base"
	}
	// A clean checkout can still hold the only copy of commits on a detached HEAD:
	// the branch stays at its base, and git worktree remove would lose them.
	if head, err := git(path, "rev-parse", "HEAD"); err != nil || head != baseSHA {
		return true, "the worktree's HEAD has moved from its base"
	}
	if out, err := git(repoRoot, "worktree", "remove", path); err != nil {
		return true, "git would not remove the worktree: " + out
	}
	if out, err := git(repoRoot, "update-ref", "-d", "refs/heads/"+branch, tip); err != nil {
		return true, "the worktree was removed but git would not delete the branch: " + out
	}
	return false, ""
}
