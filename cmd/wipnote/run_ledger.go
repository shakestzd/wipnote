package main

import (
	"fmt"
	"os"

	"github.com/shakestzd/wipnote/core/runledger"
)

// initRunLedgerCommitSeam installs the commit producer for run-ledger writes
// (feat-36658c22). core/runledger cannot import internal/commitqueue; every
// writer (the hooks and `wipnote run`) runs inside this binary.
func initRunLedgerCommitSeam() {
	runledger.OnCommit = persistRunLedgerWrite
}

// persistRunLedgerWrite records a run shard for commit like the other
// ledgers: under the default "defer" policy every append to one shard
// coalesces into one pending intent, so a session's runs are one commit.
// Hooks take the light, git-free path.
func persistRunLedgerWrite(wipnoteDir, relPath, action string) {
	msg := "wipnote: record " + action
	switch policy := workitemArtifactCommitPolicyForEnv(); {
	case policy == workitemArtifactCommitPolicyDefer || (commitIntentLight.Load() && policy != workitemArtifactCommitPolicyNone):
		err := enqueueLedgerCommitIntent(wipnoteDir, relPath, msg, "run", "run ledger commit defer")
		if err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "run ledger commit defer: %v\n", err)
		}
	default:
		if err := commitWipnotePath(wipnoteDir, relPath, msg); err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "run ledger commit: %v\n", err)
		}
	}
}
