package main

import (
	"github.com/shakestzd/wipnote/core/delegationledger"
)

// initDelegationLedgerCommitSeam installs the commit producer for canonical
// delegations-ledger writes. core/delegationledger cannot import
// internal/commitqueue, but every writer (`wipnote delegate import-orch` now,
// the delegate run and spawn verbs later) runs inside this binary, so wiring
// the seam once at startup covers them all.
func initDelegationLedgerCommitSeam() {
	delegationledger.OnCommit = persistDelegationLedgerWrite
}

// persistDelegationLedgerWrite records the delegations ledger for commit, the
// same way the gate and sessions ledgers do: under the default "defer" policy
// the commit queue coalesces every write to the one file into a single pending
// intent (AppendCoalescingByRelPath), so an import of many runs is one commit.
// Only the git commit is deferred; the row is already on disk. Under the
// legacy "separate" policy the write is committed directly; under "none" it is
// neither committed nor queued (bug-37c5cac2).
func persistDelegationLedgerWrite(wipnoteDir, relPath, action string) {
	persistLedgerWrite(wipnoteDir, relPath, "wipnote: record "+action, "delegation", "delegations ledger commit")
}
