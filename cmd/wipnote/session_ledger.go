package main

import (
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// initSessionLedgerCommitSeam installs the commit producer for canonical
// sessions-ledger writes. core/sessionledger cannot import internal/commitqueue
// (core must not depend on internal), but every writer — CLI command, hook
// handler, and retention sweep alike — runs inside this binary, so wiring the
// seam once at startup covers them all.
func initSessionLedgerCommitSeam() {
	sessionledger.OnCommit = persistSessionLedgerWrite
}

// persistSessionLedgerWrite records the sessions ledger for commit.
//
// COMMIT BATCHING: the ledger is one file that every session in the repo
// appends to, so committing each mutation directly would produce a commit per
// session start and per session end. The deferred artifact commit-queue already
// solves this and needs no changes: AppendCoalescingByRelPath drops older
// pending intents naming the same repo-relative path before appending the new
// one, so a whole session's start, end and archive collapse into a SINGLE
// pending intent that commits the file's final state once, whenever
// `wipnote commit-queue flush` next drains.
//
// Under "none" nothing is committed or queued (bug-37c5cac2). Under the
// legacy "separate" policy the write is committed directly, matching
// how work-item artifacts and the claim ledger behave under that same opt-in.
func persistSessionLedgerWrite(wipnoteDir, relPath, action string) {
	persistLedgerWrite(wipnoteDir, relPath, "wipnote: record "+action, "session", "session ledger commit")
}
