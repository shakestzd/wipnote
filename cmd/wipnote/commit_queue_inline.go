package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// commit_queue_inline.go — draining ONE work item's own deferred artifact
// commit intents inline (GH#160, bug-3cff0f93).
//
// With defer as the default policy, `start` leaves a pending intent that
// `check --gate --work-item` then refused on, forcing every agent to spend
// extra tool calls on `commit-queue flush` at task end — and `complete` queued
// yet another intent. Both now drain the calling item's own intents inline,
// under the same outbox lock and repo-scoped git lock the flush command uses,
// leaving a stranger's backlog untouched.

// workItemIntentMatcher selects the intents that belong to workItemID.
func workItemIntentMatcher(workItemID string) func(commitqueue.Intent) bool {
	return func(i commitqueue.Intent) bool { return i.WorkItemID == workItemID }
}

// hasPendingIntentFor reports whether ob currently holds an intent for
// workItemID, so callers can skip the lock/flush cycle when there is nothing
// to do.
func hasPendingIntentFor(ob *commitqueue.Outbox, workItemID string) bool {
	pending, err := ob.Pending()
	if err != nil {
		return false
	}
	match := workItemIntentMatcher(workItemID)
	for _, i := range pending {
		if match(i) {
			return true
		}
	}
	return false
}

// flushWorkItemIntentsInline drains workItemID's own pending intents through
// the production committer. It is best-effort: any failure is reported on w
// and left for the caller's existing remediation path — it never aborts the
// gate or the completion. Returns the flush result (zero when nothing ran).
func flushWorkItemIntentsInline(ob *commitqueue.Outbox, workItemID string, w io.Writer) commitqueue.FlushResult {
	if workItemID == "" || !hasPendingIntentFor(ob, workItemID) {
		return commitqueue.FlushResult{}
	}
	res, err := ob.FlushMatching(outboxCommitter, commitqueue.MaxAttempts, workItemIntentMatcher(workItemID))
	if err != nil {
		fmt.Fprintf(w, "note: inline flush of %s's deferred artifact commit intent(s) failed: %v\n", workItemID, err)
		return res
	}
	if res.Committed > 0 {
		fmt.Fprintf(w, "flushed %d deferred artifact commit intent(s) for %s inline (no manual `wipnote commit-queue flush` needed)\n",
			res.Committed, workItemID)
	}
	for _, f := range res.Failures {
		fmt.Fprintf(w, "note: deferred artifact commit for %s still failing (attempt %d): %v\n",
			workItemID, f.Intent.Attempts, f.Err)
	}
	printIgnoredIntents(w, res.Ignored)
	return res
}

// flushOwnIntentsAfterComplete is the deferred complete path's inline drain:
// after the "complete" intent is recorded it is committed immediately so the
// outbox is left empty for this item and no agent has to spend a tool call on
// `commit-queue flush`. Non-fatal by construction — the intent stays queued on
// failure exactly as before.
func flushOwnIntentsAfterComplete(wipnoteDir, id string, w io.Writer) {
	repoRoot := filepath.Dir(wipnoteDir)
	ob, err := openCommitOutbox(repoRoot)
	if err != nil {
		return
	}
	res := flushWorkItemIntentsInline(ob, id, w)
	if res.Committed == 0 {
		fmt.Fprintf(w, "artifact commit deferred by WIPNOTE_ARTIFACT_COMMIT_POLICY=defer for %s.\n  pending intent recorded; run: wipnote commit-queue flush\n", id)
	}
}
