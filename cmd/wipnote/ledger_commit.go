package main

import (
	"fmt"
	"os"
)

// ledgerCommitAction is what a ledger OnCommit seam does with one write.
type ledgerCommitAction int

const (
	// ledgerCommitSkip neither commits nor queues: WIPNOTE_ARTIFACT_COMMIT_POLICY=none.
	ledgerCommitSkip ledgerCommitAction = iota
	// ledgerCommitQueue records a coalescing commit intent for the commit queue.
	ledgerCommitQueue
	// ledgerCommitDirect commits the ledger file now (legacy "separate" policy).
	ledgerCommitDirect
)

// ledgerCommitActionForEnv maps the artifact commit policy onto a ledger write
// (bug-37c5cac2). "none" always wins: it skips commits AND queueing, even in the
// SessionEnd light mode. With honorLight, light mode queues under any other
// policy so the hook never spends its budget on a direct commit.
func ledgerCommitActionForEnv(honorLight bool) ledgerCommitAction {
	switch policy := workitemArtifactCommitPolicyForEnv(); {
	case policy == workitemArtifactCommitPolicyNone:
		return ledgerCommitSkip
	case policy == workitemArtifactCommitPolicyDefer, honorLight && commitIntentLight.Load():
		return ledgerCommitQueue
	default:
		return ledgerCommitDirect
	}
}

// persistLedgerWrite is the shared body of the run, claim, session and
// delegation ledger OnCommit seams. action tags the queued intent; label prefixes
// the debug messages ("<label> defer" for the queue, "<label>" for a commit).
func persistLedgerWrite(wipnoteDir, relPath, msg, action, label string) {
	switch ledgerCommitActionForEnv(true) {
	case ledgerCommitSkip:
		return
	case ledgerCommitQueue:
		err := enqueueLedgerCommitIntent(wipnoteDir, relPath, msg, action, label+" defer")
		if err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "%s defer: %v\n", label, err)
		}
	default:
		if err := commitWipnotePath(wipnoteDir, relPath, msg); err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "%s: %v\n", label, err)
		}
	}
}
