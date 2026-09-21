package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// Completion-scope resolution for the working-tree evidence the completion
// gates read (bug-6c953712 / GH-#161).
//
// The gates used to run `git status` against the .wipnote owner — the main
// checkout — regardless of where the completing agent was actually working.
// Under concurrent worktree dispatch that is the wrong tree: two agents share
// one .wipnote/ but implement in separate linked worktrees, so a docs-only
// item completed from worktree A inherited whatever was dirty in the main
// checkout (or in a sibling's tree) and was falsely blocked for it.
//
// The scope is resolved from the caller's CWD when that CWD belongs to the
// SAME repository as the .wipnote owner (shared git-common-dir, the same
// discriminator `wipnote history` uses). Otherwise — CWD outside the repo, a
// nested submodule, no git — it falls back to the owner, which is the
// pre-existing behaviour. There is no durable per-session record of the exec
// worktree to consult in between: the session ledger (core/sessionledger) does
// not carry it, and the sessions.exec_worktree_path column lives only in the
// per-process ephemeral projection.

// completionScope is the git tree the completion gates scan for uncommitted
// and branch-local evidence.
type completionScope struct {
	// Root is the absolute path of the worktree scanned.
	Root string
	// Branch is the branch checked out at Root, or "" when detached/unknown.
	Branch string
	// Linked is true when Root is a linked worktree rather than the .wipnote
	// owner's checkout. Only then is the branch diff consulted.
	Linked bool
	// BaseRef is the ref the branch diff was taken against; "" when no base
	// could be resolved.
	BaseRef string
}

// describe renders the scope for gate messages, e.g.
// "worktree /w/a (branch feat-a, diff against origin/main)".
func (s completionScope) describe() string {
	if s.Root == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("worktree " + s.Root)
	var details []string
	if s.Branch != "" {
		details = append(details, "branch "+s.Branch)
	}
	if s.Linked && s.BaseRef != "" {
		details = append(details, "diff against "+s.BaseRef)
	}
	if len(details) > 0 {
		b.WriteString(" (" + strings.Join(details, ", ") + ")")
	}
	return b.String()
}

// resolveCompletionWorktree picks the tree the completion gates scan: the
// caller's worktree when it is part of the same repository as repoRoot, else
// repoRoot itself.
func resolveCompletionWorktree(repoRoot string) completionScope {
	scope := completionScope{Root: repoRoot}
	root, err := resolveHistoryRoot(repoRoot)
	if err != nil || root == "" {
		return scope
	}
	scope.Root = root
	ownerAbs, _ := filepath.EvalSymlinks(repoRoot)
	rootAbs, _ := filepath.EvalSymlinks(root)
	scope.Linked = ownerAbs != "" && rootAbs != "" && ownerAbs != rootAbs
	if branch, err := gitOutputIn(root, "rev-parse", "--abbrev-ref", "HEAD"); err == nil && branch != "HEAD" {
		scope.Branch = branch
	}
	if scope.Linked {
		scope.BaseRef = branchBaseRef(root, repoRoot)
	}
	return scope
}

// branchBaseRef returns the ref a linked worktree's branch should be diffed
// against: the upstream default branch when the repo has one, else a local
// main/master, else the .wipnote owner's HEAD (the tree the worktree was most
// likely forked from). Returns "" when nothing resolves.
func branchBaseRef(root, repoRoot string) string {
	if ref := upstreamDefaultRef(root); ref != "" {
		return ref
	}
	for _, ref := range []string{"main", "master"} {
		if gitRefExists(root, "refs/heads/"+ref) {
			return ref
		}
	}
	if sha, err := gitOutputIn(repoRoot, "rev-parse", "--verify", "HEAD"); err == nil && sha != "" {
		return sha
	}
	return ""
}

// upstreamDefaultRef resolves the remote default branch: origin/HEAD when the
// remote advertises one, else origin/main or origin/master when either
// tracking ref exists. Returns "" when the repo has no usable remote ref.
func upstreamDefaultRef(root string) string {
	if ref, err := gitOutputIn(root, "symbolic-ref", "--short", "-q", "refs/remotes/origin/HEAD"); err == nil && ref != "" {
		return ref
	}
	for _, ref := range []string{"origin/main", "origin/master"} {
		if gitRefExists(root, "refs/remotes/"+ref) {
			return ref
		}
	}
	return ""
}

// gitRefExists reports whether the fully-qualified ref resolves in root.
func gitRefExists(root, ref string) bool {
	return exec.Command("git", "-C", root, "rev-parse", "--verify", "-q", ref+"^{commit}").Run() == nil
}

// branchTouchedPaths lists the source paths (outside .wipnote/) that the
// scope's branch changed since it diverged from BaseRef: committed work that
// has no item ID in its message and would otherwise be invisible to the
// provenance gate. Empty when the scope is not a linked worktree or no base
// resolves.
func branchTouchedPaths(scope completionScope) []string {
	if !scope.Linked || scope.BaseRef == "" {
		return nil
	}
	base, err := gitOutputIn(scope.Root, "merge-base", "HEAD", scope.BaseRef)
	if err != nil || base == "" {
		return nil
	}
	out, err := gitOutputIn(scope.Root, "diff", "--name-only", base+"..HEAD")
	if err != nil {
		return nil
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		p := filepath.ToSlash(strings.TrimSpace(line))
		if p == "" || isWipnotePath(p) {
			continue
		}
		files = append(files, p)
	}
	return files
}

// scannedTreeNote is the trailing sentence the completion gates append so an
// operator can see which tree the evidence came from.
func scannedTreeNote(scope completionScope) string {
	if d := scope.describe(); d != "" {
		return fmt.Sprintf(" Evidence scanned in %s.", d)
	}
	return ""
}
