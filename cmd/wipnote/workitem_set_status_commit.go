package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// commitArtifact commits the artifact HTML to the main git repo on every state
// transition so that YOLO/worktree-based runs (where .wipnote/ is suppressed by
// the per-worktree exclude) never lose the state file at any point in the
// lifecycle. The commit is non-fatal: if git is unavailable or the commit
// fails for any reason (hook rejection, nothing to commit, non-git project),
// we log to stderr and continue. State change does not depend on the commit.
// Gate with an explicit allowlist via shouldAutocommitWorkitemArtifact:
// plans have their own atomic commit path (commitPlanChange in
// plan_yaml_cmds.go) that handles YAML+HTML together.
//
// stop reports that the caller must return err immediately, skipping the
// remaining post-transition work.
func (t *statusTransition) commitArtifact() (stop bool, err error) {
	dir, typeName, id := t.dir, t.typeName, t.id
	if t.transactionalComplete {
		return t.commitArtifactTransactionally()
	} else if t.deferredComplete {
		if err := persistArtifactTransitionFn(dir, typeName, id, "complete"); err != nil {
			// Environmental (unwritable cache / read-only FS, GH#149): the
			// canonical artifact is on disk and the item is logically done —
			// do NOT reopen. Warn that the commit is pending and carry on so
			// the completion still reports (and attaches any learning).
			if !isEnvironmentalOutboxError(err) {
				return true, abortDeferredComplete(t.col, dir, typeName, id, err)
			}
			warnDeferredCommitUnavailable(os.Stderr, typeName, id, err)
		} else {
			// GH#160: drain this item's own intent right away so the outbox
			// is empty for it and no manual flush is needed. Non-fatal.
			flushOwnIntentsAfterComplete(dir, id, os.Stderr)
		}
	} else if shouldAutocommitWorkitemArtifact(typeName) {
		action := actionFromStatus(t.status)
		if err := persistWorkitemArtifactTransition(dir, typeName, id, action); err != nil {
			fmt.Fprintf(os.Stderr, "autocommit warning: %v\n", err)
		}
	}
	return false, nil
}

// commitArtifactTransactionally is the transactional complete path: a failed
// artifact commit must NOT leave the item silently "done" with no durable
// record. On failure perform a compensating re-open (status → in-progress) so
// the item's state matches reality, then exit non-zero with the exact
// remediation command. The re-open's own side effects (lineage edges, session
// events, attribution) are accepted and coherent with a re-open.
func (t *statusTransition) commitArtifactTransactionally() (stop bool, err error) {
	dir, typeName, id := t.dir, t.typeName, t.id
	cerr := commitArtifactTransactional(dir, typeName, id, t.artifactPreHead)
	if cerr == nil {
		return false, nil
	}
	// Environmental read-only filesystem: the artifact is on disk and the
	// item is logically done — do NOT reopen. Emit an advisory to stderr
	// and return nil so the item stays "done".
	if errors.Is(cerr, errReadOnlyFS) {
		relArtifact := filepath.Join(".wipnote", typeName+"s", id+".html")
		fmt.Fprintf(os.Stderr,
			"autocommit skipped: .git is read-only (sandboxed). Item marked done. "+
				"Commit manually: git add %s && git commit -m %q\n",
			relArtifact, "wipnote: complete "+id)
		sweepClaimsOnCompletion(dir, id, t.status, t.transitionStartedAt)
		return true, nil
	}
	// Compensating re-open: use col.Start, the codebase's canonical
	// revert transition.
	_, reopenErr := t.col.Start(id)
	WriteStatuslineCache(dir, id)
	remediation := fmt.Sprintf("wipnote %s complete %s", typeName, id)
	if reopenErr != nil {
		return true, fmt.Errorf(
			"completion aborted: artifact commit failed for %s (%v) and the compensating re-open ALSO failed (%v).\n"+
				"The item may be left in an inconsistent state — inspect with 'wipnote %s show %s', "+
				"manually restore the artifact, then rerun:\n  %s",
			id, cerr, reopenErr, typeName, id, remediation)
	}
	return true, fmt.Errorf(
		"completion aborted: artifact commit failed for %s: %v\n"+
			"The item has been re-opened (status: in-progress) so its state matches reality. "+
			"Resolve the commit blocker (e.g. unlock the git index, fix a rejecting hook, or commit manually), then rerun:\n  %s",
		id, cerr, remediation)
}
