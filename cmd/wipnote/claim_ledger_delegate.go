package main

import (
	"fmt"
	"io"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateRunEnv names the delegate run a worker session belongs to. spawn's
// boot line sets it in the worker's pane (plan-28b4a7f6 slice 10).
const delegateRunEnv = session.RunEnv

// delegationFamily returns the sessions the claim gate treats as the
// caller's own family through the delegations ledger (plan-28b4a7f6
// slice 16, q-claim-handoff answered co-claim-gate):
//
//   - the caller is an orchestrator: every session in worker_sessions of an
//     open interactive row whose caller_session is the caller;
//   - the caller is a worker: runID (its WIPNOTE_DELEGATE_RUN) names an open
//     interactive row, whose caller_session and worker sessions (the same
//     worker before a /clear) are family.
//
// The env var is trusted only when it names an open interactive row, as
// --force is trusted: the gate is cooperative. ok is false when the ledger
// exists but cannot be read; the gate then fails open.
func delegationFamily(wipnoteDir, callerSessionID, runID string) (map[string]bool, bool) {
	open, ok := delegationledger.NewStore(wipnoteDir).ReadOpen()
	if !ok {
		return nil, false
	}
	family := map[string]bool{}
	add := func(r delegationledger.Record) {
		for _, s := range r.WorkerSessions {
			family[s] = true
		}
	}
	for _, r := range open.Interactive() {
		if callerSessionID != "" && r.CallerSession == callerSessionID {
			add(r)
		}
	}
	if r, found := open.ByRunID(runID); runID != "" && found && r.Mode == delegationledger.ModeInteractive {
		add(r)
		if r.CallerSession != "" {
			family[r.CallerSession] = true
		}
	}
	return family, true
}

// warnLiveDelegation warns, without refusing, that completing workItemID
// will also close the claim of a live delegate worker on it: completion
// closes every open episode on the item (CloseWorkItem). It reads only the
// ledger, so it never waits on herdr; an unreadable ledger warns nothing.
func warnLiveDelegation(w io.Writer, wipnoteDir, workItemID string) {
	open, ok := delegationledger.NewStore(wipnoteDir).ReadOpen()
	if !ok {
		return
	}
	for _, r := range open.InteractiveForItem(workItemID) {
		fmt.Fprintf(w, "warning: %s has a live delegate worker %q (run %s); completing closes its claim too.\n"+
			"  Stop it first: wipnote delegate stop %s\n", workItemID, r.Name, r.ID, r.Name)
	}
}

// expireDelegateWorkerClaims is the delegate verbs' worker-loss close: one
// lost worker session's episodes end as expired, nobody else's.
func expireDelegateWorkerClaims(wipnoteDir string) func(sessionID string) {
	return func(sessionID string) {
		if _, err := hooks.CloseClaimEpisodesForSession(wipnoteDir, sessionID, claimledger.OutcomeExpired); err != nil {
			claimLedgerWarn("expire claims of lost worker session %s: %v", sessionID, err)
		}
	}
}
