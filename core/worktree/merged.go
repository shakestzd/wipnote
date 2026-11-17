package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Merged states reported for a worktree's branch.
const (
	MergedAncestor = "merged"
	MergedSquash   = "merged (squash)"
	MergedNot      = "not merged"
	MergedNoBranch = "no branch"
)

// Entry is one worktree of the repo with the facts list and clean act on.
type Entry struct {
	Path       string `json:"path"`
	RelPath    string `json:"rel_path"`
	Branch     string `json:"branch"`
	Head       string `json:"head"`
	Main       bool   `json:"main"`
	Current    bool   `json:"current"`
	Locked     bool   `json:"locked"`
	LockReason string `json:"lock_reason,omitempty"`
	Tracked    bool   `json:"tracked_changes"`
	Untracked  bool   `json:"untracked_files"`
	Merged     string `json:"merged"`
	WorkItem   string `json:"work_item,omitempty"`
	Removable  bool   `json:"removable"`
	keep       string // why a merged branch must stay anyway (protectReason)
	Reason     string `json:"reason"`
	Action     string `json:"action,omitempty"`
}

// ghMergedPR reports a merged PR for branch whose head equals tip. A var so
// tests can stand in for the network.
var ghMergedPR = defaultGhMergedPR

var workItemRe = regexp.MustCompile(`\b(?:feat|bug|spk|trk)-[0-9a-f]{8}\b`)

// DefaultRef finds the ref merged work is measured against: origin's HEAD,
// else origin/main, origin/master, main, master.
func DefaultRef(repoRoot string) string {
	if out, err := git(repoRoot, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && out != "" {
		return out
	}
	for _, ref := range []string{"origin/main", "origin/master", "main", "master"} {
		if _, err := git(repoRoot, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			return ref
		}
	}
	return ""
}

// MergedState says how tip is merged into defaultRef, by the first rule that
// matches: ancestry, a merge-tree equal to the default tree (squash), then a
// merged GitHub PR whose head equals tip.
func MergedState(repoRoot, defaultRef, branch, tip string) string {
	if defaultRef != "" {
		if _, err := git(repoRoot, "merge-base", "--is-ancestor", tip, defaultRef); err == nil {
			return MergedAncestor
		}
		if squashMerged(repoRoot, defaultRef, tip) {
			return MergedSquash
		}
	}
	if n, ok := ghMergedPR(repoRoot, branch, tip); ok {
		return fmt.Sprintf("merged (PR #%d)", n)
	}
	return MergedNot
}

func squashMerged(repoRoot, defaultRef, tip string) bool {
	out, err := exec.Command("git", "-C", repoRoot, "merge-tree", "--write-tree", defaultRef, tip).Output()
	if err != nil {
		return false
	}
	tree, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	want, err := git(repoRoot, "rev-parse", defaultRef+"^{tree}")
	return err == nil && tree != "" && tree == want
}

func defaultGhMergedPR(repoRoot, branch, tip string) (int, bool) {
	if _, err := exec.LookPath("gh"); err != nil {
		return 0, false
	}
	if url, err := git(repoRoot, "remote", "get-url", "origin"); err != nil || !strings.Contains(url, "github.com") {
		return 0, false
	}
	// Run in the repo so gh resolves its GitHub remote, not the caller's cwd.
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--state", "merged",
		"--json", "number,headRefOid")
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		return 0, false
	}
	var prs []struct {
		Number     int    `json:"number"`
		HeadRefOid string `json:"headRefOid"`
	}
	if json.Unmarshal(out, &prs) != nil {
		return 0, false
	}
	for _, pr := range prs {
		if pr.HeadRefOid == tip {
			return pr.Number, true
		}
	}
	return 0, false
}

// ListWorktrees returns every worktree of the repo; cwd marks the current one.
func ListWorktrees(repoRoot, cwd string) ([]Entry, error) {
	out, err := git(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w: %s", err, out)
	}
	entries := parseWorktreeList(out)
	defaultRef := DefaultRef(repoRoot)
	current := realPath(cwd)
	if top, err := git(cwd, "rev-parse", "--show-toplevel"); err == nil {
		current = realPath(top)
	}
	for i := range entries {
		e := &entries[i]
		e.Main = i == 0
		e.Current = realPath(e.Path) == current
		e.RelPath = relTo(repoRoot, e.Path)
		e.WorkItem = workItemRe.FindString(e.Branch + " " + filepath.Base(e.Path))
		inspectEntry(repoRoot, defaultRef, e)
	}
	return entries, nil
}

func inspectEntry(repoRoot, defaultRef string, e *Entry) {
	if e.Branch == "" {
		e.Merged = MergedNoBranch
	} else {
		e.Merged = MergedState(repoRoot, defaultRef, e.Branch, e.Head)
		e.keep = protectReason(repoRoot, defaultRef, e)
	}
	if _, err := os.Stat(e.Path); err != nil {
		e.Reason = "directory missing (run git worktree prune)"
		return
	}
	status, err := git(e.Path, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		e.Reason = "git status failed"
		return
	}
	for line := range strings.SplitSeq(status, "\n") {
		if strings.HasPrefix(line, "?? ") {
			e.Untracked = true
		} else if strings.TrimSpace(line) != "" {
			e.Tracked = true
		}
	}
	e.Reason = keepReason(e)
	e.Removable = e.Reason == ""
	if e.Removable {
		e.Reason = "clean, " + e.Merged
	}
}

func keepReason(e *Entry) string {
	switch {
	case e.Main:
		return "main checkout"
	case e.Current:
		return "current worktree"
	case e.Locked:
		return "locked"
	case e.Tracked:
		return "tracked changes"
	case e.Untracked:
		return "untracked files"
	case e.keep != "":
		return e.keep
	case e.Branch == "":
		return "no branch (detached HEAD)"
	case e.Merged == MergedNot:
		return "not merged"
	}
	return ""
}

// protectReason keeps branches that count as merged but must never be
// removed: the default branch itself (a release worktree on main is an
// ancestor of origin/main, and clean would delete main), and a branch with no
// commits of its own, which is a just-created worktree whose worker has not
// committed yet rather than finished work.
func protectReason(repoRoot, defaultRef string, e *Entry) string {
	local := defaultRef
	if _, name, ok := strings.Cut(defaultRef, "/"); ok {
		local = name
	}
	if e.Branch == local || e.Branch == "main" || e.Branch == "master" {
		return "default branch"
	}
	if defaultRef == "" {
		return ""
	}
	if tip, err := git(repoRoot, "rev-parse", defaultRef+"^{commit}"); err == nil && tip == e.Head {
		return "no commits yet"
	}
	return ""
}

// CleanWorktrees lists worktrees and, when apply is set, removes each
// removable one and its local branch, then prunes. Never forces, never
// touches remotes.
func CleanWorktrees(repoRoot, cwd string, apply bool) ([]Entry, error) {
	entries, err := ListWorktrees(repoRoot, cwd)
	if err != nil {
		return nil, err
	}
	if !apply {
		return entries, nil
	}
	for i := range entries {
		e := &entries[i]
		if !e.Removable {
			continue
		}
		if err := RemoveManagedWorktree(repoRoot, e.Path); err != nil {
			e.Removable, e.Reason, e.Action = false, "remove failed: "+firstLine(err.Error()), "kept"
			continue
		}
		e.Action = "removed"
		// -D is safe only because the merged check passed above.
		if _, err := git(repoRoot, "branch", "-D", e.Branch); err != nil {
			e.Action = "removed (branch kept: delete failed)"
		}
	}
	_, _ = git(repoRoot, "worktree", "prune")
	return entries, nil
}

func parseWorktreeList(out string) []Entry {
	var entries []Entry
	for block := range strings.SplitSeq(strings.TrimSpace(out), "\n\n") {
		var e Entry
		for line := range strings.SplitSeq(block, "\n") {
			key, val, _ := strings.Cut(line, " ")
			switch key {
			case "worktree":
				e.Path = val
			case "HEAD":
				e.Head = val
			case "branch":
				e.Branch = strings.TrimPrefix(val, "refs/heads/")
			case "locked":
				e.Locked, e.LockReason = true, val
			}
		}
		if e.Path != "" {
			entries = append(entries, e)
		}
	}
	return entries
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

func relTo(root, p string) string {
	rel, err := filepath.Rel(realPath(root), realPath(p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return p
	}
	return rel
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
