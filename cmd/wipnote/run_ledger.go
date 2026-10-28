package main

import (
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
// Hooks take the light, git-free path; "none" skips both (bug-37c5cac2).
func persistRunLedgerWrite(wipnoteDir, relPath, action string) {
	persistLedgerWrite(wipnoteDir, relPath, "wipnote: record "+action, "run", "run ledger commit")
}
