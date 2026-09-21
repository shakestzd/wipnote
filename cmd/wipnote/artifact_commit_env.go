package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"github.com/shakestzd/wipnote/core/workitem"
)

// artifact_commit_env.go — classification of ENVIRONMENTAL failures on the
// deferred artifact-commit path (GH#149, bug-1410fb35).
//
// The deferred complete path records a commit intent in the per-user cache
// dir. In a Codex workspace-write sandbox (or any read-only HOME) that dir is
// unwritable, so the enqueue fails with EPERM/EACCES/EROFS even though the
// canonical .wipnote artifact — the actual source of truth — was written
// fine. Treating that as fatal reopened a genuinely completed item. The
// legacy transactional path already tolerates a read-only git index
// (errReadOnlyFS); this mirrors that tolerance for the outbox.

// isEnvironmentalOutboxError reports whether err is a permission / read-only
// filesystem failure from the outbox (EPERM, EACCES, EROFS), as opposed to a
// real queue problem (corrupt file, invalid intent) that should still abort.
func isEnvironmentalOutboxError(err error) bool {
	if err == nil {
		return false
	}
	// os.ErrPermission matches both EACCES and EPERM via syscall.Errno.Is.
	return errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EROFS)
}

// persistArtifactTransitionFn is the injection seam for the deferred complete
// path (mirrors strictCommitFn on the transactional path). Tests override it
// to inject an environmental outbox failure without a real read-only cache.
var persistArtifactTransitionFn = persistWorkitemArtifactTransition

// abortDeferredComplete is the compensating re-open for a REAL (non-
// environmental) failure to queue the deferred artifact commit: the item is
// moved back to in-progress so its state matches reality and the exact
// remediation command is returned.
func abortDeferredComplete(col *workitem.Collection, dir, typeName, id string, cause error) error {
	_, reopenErr := col.Start(id)
	WriteStatuslineCache(dir, id)
	remediation := fmt.Sprintf("wipnote %s complete %s", typeName, id)
	if reopenErr != nil {
		return fmt.Errorf(
			"completion aborted: failed to queue deferred artifact commit for %s (%v) and the compensating re-open ALSO failed (%v).\n"+
				"The item may be left in an inconsistent state — inspect with 'wipnote %s show %s', then rerun:\n  %s",
			id, cause, reopenErr, typeName, id, remediation)
	}
	return fmt.Errorf(
		"completion aborted: failed to queue deferred artifact commit for %s: %v\n"+
			"The item has been re-opened (status: in-progress). Resolve the queue/outbox problem, then rerun:\n  %s",
		id, cause, remediation)
}

// warnDeferredCommitUnavailable prints the pending-sync warning emitted when
// the artifact is persisted but the commit queue could not be reached. The
// item stays done; the operator commits by hand or flushes later.
func warnDeferredCommitUnavailable(w io.Writer, typeName, id string, cause error) {
	fmt.Fprintf(w,
		"warning: artifact persisted for %s; commit-queue unavailable (%v).\n"+
			"  Item marked done. Commit .wipnote manually (git add %s && git commit -m %q) "+
			"or run `wipnote commit-queue flush` later; set WIPNOTE_ARTIFACT_COMMIT_POLICY=none to skip queueing.\n",
		id, cause, workitemArtifactRelPath(typeName, id), workitemArtifactCommitMessage(id, "complete"))
}
