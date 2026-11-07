package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/core/workitem"
)

// Merged-upstream completion gate (feat-be42685c / GH-#93).
//
// The provenance gate accepts any linked commit reachable by `git log --all`,
// so work sitting on an unmerged worktree branch still let an item be marked
// done: wipnote said "done" while origin/main carried no trace of the
// deliverable, and orphan branches accrued silently until an audit found
// them. This gate adds the missing precondition — at least one linked commit
// must be an ancestor of the upstream default branch — with an audited
// --allow-orphan override for work that is branch-local by design.

// wiAllowOrphan carries the --allow-orphan reason. When non-empty it
// overrides the merged-upstream gate and the rationale is persisted on the
// artifact under allowOrphanMarker.
var wiAllowOrphan string

// allowOrphanMarker prefixes the content note the override rationale is
// persisted under, mirroring acceptedAdvisoryMarker so audit tooling reads
// both the same way.
const allowOrphanMarker = "allow-orphan (unmerged-upstream override): "

// checkMergedUpstreamCompleteGate refuses completion of a code-bearing item
// whose linked commits are all absent from the upstream default branch.
//
// Skipped (returns nil) when: the project is not a git repo; no upstream
// default ref resolves (no remote); the item has no linked commits (the
// provenance gate owns that case, including its --accepted-advisory path); or
// the item is not code-bearing. Runs AFTER the provenance gate, before any
// state transition.
func checkMergedUpstreamCompleteGate(col *workitem.Collection, ev *completionEvidence, typeName, id, allowOrphan string) error {
	repoRoot := ev.repoRoot
	if repoRoot == "" || !isGitRepo(repoRoot) {
		return nil
	}
	upstream := upstreamDefaultRef(repoRoot)
	if upstream == "" {
		return nil
	}
	commits := ev.linkedCommits()
	if len(commits) == 0 {
		return nil
	}
	if paths, _ := ev.codeBearingPaths(); len(paths) == 0 {
		return nil
	}
	refs := completionRefs(repoRoot, upstream, resolveCompletionBase(completionConfigDir(ev)))
	for _, sha := range commits {
		for _, ref := range refs {
			if commitReachableFromUpstream(repoRoot, sha, ref) {
				return nil
			}
		}
	}

	allowOrphan = strings.TrimSpace(allowOrphan)
	if allowOrphan == "" {
		return mergedGateRefusal(typeName, id, repoRoot, commits, refs)
	}

	// Audited override: persist BEFORE col.Complete so the note survives the
	// completion flush and lands in the transactional artifact commit.
	if err := col.Edit(id).AddNote(allowOrphanMarker + allowOrphan).Save(); err != nil {
		return fmt.Errorf("merged-upstream gate: record allow-orphan on %s: %w", id, err)
	}
	fmt.Fprintf(os.Stderr,
		"allow-orphan warning: completing %s %s with no linked commit reachable from %s.\n  reason: %s\n",
		typeName, id, strings.Join(refs, ", "), allowOrphan)
	return nil
}

// commitReachableFromUpstream reports whether sha is an ancestor of (or equal
// to) upstream. A sha absent from this checkout is simply not reachable.
func commitReachableFromUpstream(repoRoot, sha, upstream string) bool {
	return exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", sha, upstream).Run() == nil
}

// completionConfigDir is the project directory holding .wipnote/config.json.
func completionConfigDir(ev *completionEvidence) string {
	if ev.wipnoteDir != "" {
		return filepath.Dir(ev.wipnoteDir)
	}
	return ev.repoRoot
}

// mergedGateRefusal builds the refusal naming every ref checked. A local
// default branch that was not accepted gets the completion_base hint.
func mergedGateRefusal(typeName, id, repoRoot string, commits, refs []string) error {
	hint := ""
	if len(refs) == 1 && localDefaultBranch(repoRoot) != "" {
		hint = "Local-only project that merges into its local default branch without pushing? Set `completion_base: \"local\"` in .wipnote/config.json (or WIPNOTE_COMPLETION_BASE=local).\n"
	}
	return fmt.Errorf(
		"refusing to complete %s %s: none of its %d linked commit(s) (e.g. %s) is reachable from %s — "+
			"the work has not been merged upstream.\n%s"+
			"Open a pull request for the branch and merge it (e.g. `gh pr create`), or fetch the merged upstream, then rerun:\n  wipnote %s complete %s\n"+
			"To intentionally complete branch-local work (records an audited rationale on the artifact), rerun with:\n"+
			"  wipnote %s complete %s --allow-orphan \"<reason>\"",
		typeName, id, len(commits), slug.Truncate(commits[0], 12), strings.Join(refs, ", "),
		hint, typeName, id, typeName, id)
}
