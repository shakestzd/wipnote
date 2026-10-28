package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Claims (plan-28b4a7f6 slice 16, docs/design/delegate.md section 5). The
// worker claims its item through the normal `wipnote <kind> start` path, so
// this file only words the brief's first line, keeps one live worker per
// item, and closes a lost worker's episodes as expired. The family rule of
// the claim gate lives with the gate (cmd/wipnote/claim_ledger_delegate.go).

// ClaimLine is the brief's first line: the worker's own claim of its item.
func ClaimLine(workItem string) string {
	return fmt.Sprintf("First claim your work item: run `wipnote %s start %s`.\n", workitem.KindForID(workItem), workItem)
}

// checkLive refuses a spawn whose name, or whose work item, an open
// interactive row holds while its pane lives (one worker per item, v1). A
// row whose pane herdr says is gone goes through the worker-loss path first
// and no longer counts. One-shot rows never count: they do not claim.
func checkLive(ctx context.Context, d Deps, o SpawnOptions) error {
	open, ok := d.Ledger.ReadOpen()
	if !ok {
		return errors.New("delegate: the delegations ledger cannot be read; nothing was spawned")
	}
	for _, r := range open.Interactive() {
		sameName, sameItem := r.Name == o.Name, r.WorkItem == o.WorkItem
		if !sameName && !sameItem {
			continue
		}
		rf, info, err := loadInfo(d.Store, r.ID)
		if err == nil && info.Pane != "" {
			if _, gerr := d.Herdr.Get(ctx, herdr.PaneID(info.Pane)); herdr.IsAbsent(gerr) {
				loseWorker(d, r, rf)
				continue
			}
		}
		if sameName {
			return fmt.Errorf("%w: %s is run %s", ErrLiveName, o.Name, r.ID)
		}
		return fmt.Errorf("%w: %s is held by worker %s (run %s); stop it first, one worker per item", ErrLiveWorker, o.WorkItem, r.Name, r.ID)
	}
	return nil
}

// loseWorker is the worker-loss path: the pane is gone, so no worker process
// is left under herdr and no SessionEnd will fire. The row closes as gone,
// then each of the worker's sessions has its episodes closed as expired,
// only that session's (Deps.ExpireClaims). The caller's episode is never
// touched.
func loseWorker(d Deps, r delegationledger.Record, rf *runstore.RunFolder) {
	_, _ = d.Ledger.CloseIfOpen(r.ID, delegationledger.CloseInfo{Ended: d.now(), Reason: delegationledger.EndGone})
	expireWorkerClaims(d, r, rf)
}

// expireWorkerClaims closes as expired the episodes of every session the run
// used: those in worker_sessions and those bound in events that no list has
// moved into the ledger yet (the worker claims before that).
func expireWorkerClaims(d Deps, r delegationledger.Record, rf *runstore.RunFolder) {
	if d.ExpireClaims == nil {
		return
	}
	sessions := r.WorkerSessions
	if rf != nil {
		if evs, err := rf.ReadEvents(); err == nil {
			sessions = mergeSessions(sessions, factsOf(evs).sessions)
		}
	}
	for _, sid := range sessions {
		d.ExpireClaims(sid)
	}
}
