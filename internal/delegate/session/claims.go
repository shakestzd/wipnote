package session

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Claims (plan-28b4a7f6 slice 16, docs/design/delegate.md section 5). The
// worker claims its item through the normal `wipnote <kind> start` path, so
// this file only words the brief's first line, keeps one live worker per
// item, and closes a lost worker's episodes as expired. The family rule of
// the claim gate lives with the gate (cmd/wipnote/claim_ledger_delegate.go).

// ClaimLine is the first line of the brief and of every sent task: the
// worker's own claim of its item. Every task says it, because a worker spawned
// without a brief gets its first task by send, and a /clear ends the session
// whose claim the old task made. Saying it again is harmless: a repeat start
// by a session that holds the item renews its open episode.
func ClaimLine(workItem string) string {
	return fmt.Sprintf("First claim your work item: run `wipnote %s start %s`.\n", workitem.KindForID(workItem), workItem)
}

// TaskPreamble is what goes before a task's text, in the brief and in every
// sent task: the claim, then the result preamble.
func TaskPreamble(info Info, mailDir string) string {
	return ClaimLine(info.WorkItem) + Preamble(info, mailDir)
}

// paneWindow bounds how long a spawn may take from opening its row to
// recording its pane; an open row with no pane older than that is lost.
const paneWindow = 5 * time.Minute

// checkLive refuses a spawn whose name, or whose work item, an open
// interactive row holds while its worker lives (one worker per item, v1). A
// lost worker (workerLost: pane gone, pane reused, or harness exited) is
// closed first and no longer counts; it is returned so the spawn can unplace
// it (checkLive runs under the caller's workers lock and namesLock, so it
// must take neither). So is a row that never recorded a pane within
// paneWindow (its spawn died before placing). Running
// under namesLock (claimName) means two spawns cannot both pass for one item.
// One-shot rows never count: they do not claim.
func checkLive(ctx context.Context, d Deps, o SpawnOptions) ([]lostWorker, error) {
	open, ok := d.Ledger.ReadOpen()
	if !ok {
		return nil, errors.New("delegate: the delegations ledger cannot be read; nothing was spawned")
	}
	var lost []lostWorker
	for _, r := range open.Interactive() {
		sameName, sameItem := r.Name == o.Name, r.WorkItem == o.WorkItem
		if !sameName && !sameItem {
			continue
		}
		rf, info, err := loadInfo(d.Store, r.ID)
		if err == nil && info.Pane != "" {
			if ob, _ := observe(ctx, d, rf, info); workerLost(ob) {
				closeLost(d, r, rf)
				lost = append(lost, lostWorker{info, ob})
				continue
			}
		} else if d.now().Sub(r.Started) > paneWindow { // never placed: its spawn died
			closeLost(d, r, rf)
			continue
		}
		if sameName {
			return lost, fmt.Errorf("%w: %s is run %s", ErrLiveName, o.Name, r.ID)
		}
		return lost, fmt.Errorf("%w: %s is held by worker %s (run %s); stop it first, one worker per item", ErrLiveWorker, o.WorkItem, r.Name, r.ID)
	}
	return lost, nil
}

// closeLost closes the row as gone and, only when this call closed it,
// finishes the run folder (so readers stop waiting for an exit and the sweep
// can age it out) and closes as expired the episodes of each of the worker's
// sessions, only that session's. A row someone else closed first (a stop
// always wins) is left as it is. The orchestrator's episode is never touched.
func closeLost(d Deps, r delegationledger.Record, rf *runstore.RunFolder) {
	closed, err := closeWorkerRow(d, r.ID, delegationledger.EndGone)
	if err != nil || !closed {
		return
	}
	if rf != nil {
		_ = rf.Finish(1, d.now())
	}
	expireWorkerClaims(d, r, rf)
}

// expireWorkerClaims closes as expired the episodes of every session the run
// used: those in worker_sessions and those bound in events that no list has
// moved into the ledger yet (the worker claims before that).
func expireWorkerClaims(d Deps, r delegationledger.Record, rf *runstore.RunFolder) {
	if d.ExpireClaims == nil && d.Claims == nil {
		return
	}
	sessions := r.WorkerSessions
	if rf != nil {
		if evs, err := rf.ReadEvents(); err == nil {
			sessions = mergeSessions(sessions, factsOf(evs).sessions)
		}
	}
	for _, sid := range sessions {
		if d.ExpireClaims != nil {
			d.ExpireClaims(sid)
			continue
		}
		_, _ = d.Claims.CloseAllForSession(sid, sid, claimledger.OutcomeExpired, d.now())
	}
}
