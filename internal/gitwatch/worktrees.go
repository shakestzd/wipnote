package gitwatch

// Worktrees and why each one may be stale. Reports only: nothing here
// removes, prunes or edits a worktree or branch.
//
// Ported in part (MIT; see THIRD-PARTY-NOTICES.md) from gh-poi
// (seachicken/gh-poi) commit abf237be0d79d754922f3cf72a905f9854d2b096,
// Copyright (c) 2021 Seito Tanaka: parseWorktrees from conn/command.go, and
// isFullyMerged from cmd/root.go. gh-poi's isFullyMerged matches the local
// head against a merged PR's commits; this one answers the same question
// from local git (merged into base, or its changes already in base after a
// squash), keeping gh-poi's rule that a branch with no commits of its own is
// not merged.

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// DefaultStaleDays is the idle threshold when gitwatch.stale_days is unset.
const DefaultStaleDays = 7

// Stale reasons, in the order they are reported.
const (
	StaleMerged       = "merged"        // merged into base, including a squash merge
	StaleUpstreamGone = "upstream_gone" // its remote branch was deleted
	StaleIdle         = "idle"          // no commits for gitwatch.stale_days days
	StalePRClosed     = "pr_closed"     // its PR was closed without merging
)

// Worktree is one git worktree. Path is "." for the main worktree,
// repo-relative inside it, else the directory's base name: never a host path.
type Worktree struct {
	Path     string   `json:"path"`
	Branch   string   `json:"branch,omitempty"`
	Main     bool     `json:"main,omitempty"`
	Locked   bool     `json:"locked,omitempty"`
	Prunable bool     `json:"prunable,omitempty"`
	IdleDays int      `json:"idle_days"`
	Stale    []string `json:"stale,omitempty"`
}

// StaleDays reads gitwatch.stale_days from the layered config (user, then
// projectDir's project and local files); unset or not positive is
// DefaultStaleDays.
func StaleDays(projectDir string) int {
	var c struct {
		StaleDays int `json:"stale_days"`
	}
	paths.ReadLayeredConfig(projectDir, paths.LayeredOptions{Section: "gitwatch"}, &c)
	if c.StaleDays <= 0 {
		return DefaultStaleDays
	}
	return c.StaleDays
}

// ReadWorktrees lists the worktrees of the repository in dir ("" is the
// working directory) with their stale reasons against base ("" is main).
// A reason that cannot be checked is skipped with a note; only an unreadable
// worktree or branch list is an error. pr_closed asks gh once, in the
// working directory.
func ReadWorktrees(ctx context.Context, r Runner, dir, base string, now time.Time) ([]Worktree, []string, error) {
	out, err := gitOut(ctx, r, dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, nil, err
	}
	raws := parseWorktrees(out)
	rows, _, err := readRefs(ctx, r, dir, "")
	if err != nil {
		return nil, nil, err
	}
	if base == "" {
		base = "main"
	}
	sc := &staleCheck{ctx: ctx, r: r, dir: dir, base: base, now: now, refs: map[string]refRow{}}
	for _, row := range rows {
		sc.refs[row.name] = row
	}
	sc.prepare(raws)
	wts := make([]Worktree, 0, len(raws))
	for _, raw := range raws {
		wts = append(wts, sc.worktree(raws[0].path, raw))
	}
	return wts, sc.notes, nil
}

// staleCheck holds what every worktree's check shares.
type staleCheck struct {
	ctx      context.Context
	r        Runner
	dir      string
	base     string // branch name, e.g. main
	baseRef  string // resolved, e.g. origin/main; "" skips merged
	now      time.Time
	days     int
	refs     map[string]refRow
	closed   map[string]bool
	firstPar map[string]bool // shas on base's first-parent line
	notes    []string
}

// prepare resolves the base, the idle threshold and, when any worktree has
// a branch to judge, the closed-PR list.
func (sc *staleCheck) prepare(raws []rawWorktree) {
	sc.days = DefaultStaleDays
	if len(raws) > 0 {
		sc.days = StaleDays(raws[0].path)
	}
	ref, err := ResolveBase(sc.ctx, sc.r, sc.dir, sc.base)
	if err != nil {
		sc.notes = append(sc.notes, StaleMerged+" not checked: "+err.Error())
	}
	sc.baseRef = ref
	for _, raw := range raws {
		if sc.judged(raw.branch) {
			sc.closed, err = closedPRBranches(sc.ctx, sc.r)
			if err != nil {
				sc.notes = append(sc.notes, StalePRClosed+" not checked: "+err.Error())
			}
			return
		}
	}
}

// judged reports whether a worktree's branch gets stale reasons: not the
// base itself, not a detached HEAD.
func (sc *staleCheck) judged(branch string) bool {
	return branch != "" && branch != sc.base
}

func (sc *staleCheck) worktree(root string, raw rawWorktree) Worktree {
	w := Worktree{Path: displayPath(root, raw.path), Branch: raw.branch, Main: raw.main,
		Locked: raw.locked, Prunable: raw.prunable}
	row, ok := sc.refs[raw.branch]
	if !ok {
		return w
	}
	idle := sc.now.Sub(time.Unix(row.committed, 0))
	w.IdleDays = max(0, int(idle.Hours()/24))
	if !sc.judged(raw.branch) {
		return w
	}
	if sc.baseRef != "" && sc.isFullyMerged(row) {
		w.Stale = append(w.Stale, StaleMerged)
	}
	if determineSyncStatus(row.track, row.upstream) == SyncDeletedAtRemote {
		w.Stale = append(w.Stale, StaleUpstreamGone)
	}
	if idle >= time.Duration(sc.days)*24*time.Hour {
		w.Stale = append(w.Stale, StaleIdle)
	}
	if sc.closed[raw.branch] {
		w.Stale = append(w.Stale, StalePRClosed)
	}
	return w
}

// isFullyMerged reports whether the branch's work is already in base: its
// tip is reachable from base, or merging it into base would change nothing
// (a squash merge). Like gh-poi, a branch with no commits of its own (its
// tip sits on base's first-parent line) is not merged.
func (sc *staleCheck) isFullyMerged(row refRow) bool {
	if sc.onFirstParent(row.sha) {
		return false
	}
	_, _, code, err := gitRun(sc.ctx, sc.r, sc.dir, "merge-base", "--is-ancestor", row.sha, sc.baseRef)
	if err == nil && code == 0 {
		return true
	}
	// merge-tree --write-tree (git 2.38+) writes no ref and touches no
	// worktree; on older git it fails and squash merges go undetected.
	tree, err := gitOut(sc.ctx, sc.r, sc.dir, "merge-tree", "--write-tree", sc.baseRef, row.sha)
	if err != nil {
		return false
	}
	baseTree, err := gitOut(sc.ctx, sc.r, sc.dir, "rev-parse", sc.baseRef+"^{tree}")
	return err == nil && firstLine([]byte(tree)) == strings.TrimSpace(baseTree)
}

func (sc *staleCheck) onFirstParent(sha string) bool {
	if sc.firstPar == nil {
		sc.firstPar = map[string]bool{}
		out, _ := gitOut(sc.ctx, sc.r, sc.dir, "rev-list", "--first-parent", sc.baseRef)
		for _, s := range strings.Fields(out) {
			sc.firstPar[s] = true
		}
	}
	return sc.firstPar[sha]
}

// closedPRBranches asks gh once for closed PRs and returns the head branches
// closed without a merge. gh's closed state includes merged PRs; a branch
// with any merged PR is left out.
func closedPRBranches(ctx context.Context, r Runner) (map[string]bool, error) {
	var list []struct {
		HeadRefName string `json:"headRefName"`
		State       string `json:"state"`
	}
	if err := ghJSON(ctx, r, &list, "pr", "list", "--state", "closed", "--json", "headRefName,state", "--limit", "100"); err != nil {
		return nil, err
	}
	closed, merged := map[string]bool{}, map[string]bool{}
	for _, pr := range list {
		switch pr.State {
		case "CLOSED":
			closed[pr.HeadRefName] = true
		case "MERGED":
			merged[pr.HeadRefName] = true
		}
	}
	for b := range merged {
		delete(closed, b)
	}
	return closed, nil
}

// displayPath is p relative to the main worktree root when inside it ("."
// for the root itself), else p's base name.
func displayPath(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Base(p)
	}
	return filepath.ToSlash(rel)
}

// rawWorktree is one entry of git worktree list --porcelain.
type rawWorktree struct {
	path, branch           string
	main, locked, prunable bool
}

// parseWorktrees is gh-poi's: the first entry is the main worktree; a
// detached HEAD has no branch.
func parseWorktrees(output string) []rawWorktree {
	results := []rawWorktree{}
	var current *rawWorktree
	isFirst := true
	for _, line := range strings.Split(output, "\n") {
		if path, ok := strings.CutPrefix(line, "worktree "); ok {
			if current != nil {
				results = append(results, *current)
			}
			current = &rawWorktree{path: path, main: isFirst}
			isFirst = false
		} else if current == nil {
			continue
		} else if branch, ok := strings.CutPrefix(line, "branch refs/heads/"); ok {
			current.branch = branch
		} else if line == "locked" || strings.HasPrefix(line, "locked ") {
			current.locked = true
		} else if strings.HasPrefix(line, "prunable ") {
			current.prunable = true
		}
	}
	if current != nil {
		results = append(results, *current)
	}
	return results
}

// addBranchHealth fills s.Branch and s.Worktrees from the repository in the
// working directory, against the PR's base when there is one, else main.
// A failure leaves the section nil and appends a note; it never fails the
// snapshot.
func addBranchHealth(ctx context.Context, r Runner, s *Snapshot, now func() time.Time) {
	if ctx.Err() != nil {
		return
	}
	base := ""
	if s.PR != nil {
		base = s.PR.Base
	}
	if b, err := ReadBranch(ctx, r, "", base); err != nil {
		s.Notes = append(s.Notes, "branch health not read: "+err.Error())
	} else {
		s.Branch = b
		if b.Base == "" {
			s.Notes = append(s.Notes, "branch health: base not found, ahead/behind not read")
		}
	}
	wts, notes, err := ReadWorktrees(ctx, r, "", base, now())
	if err != nil {
		s.Notes = append(s.Notes, "worktrees not read: "+err.Error())
		return
	}
	s.Worktrees = wts
	s.Notes = append(s.Notes, notes...)
}
