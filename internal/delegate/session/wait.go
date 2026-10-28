package session

import (
	"context"
	"errors"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// Wait outcomes. The readiness states a wait settles on keep their names.
const (
	OutcomeReady        = string(StateReady)
	OutcomeDone         = string(StateDone)
	OutcomeIdleNoResult = string(StateIdleNoResult)
	OutcomeIdleShell    = string(StateIdleShell)
	OutcomeBlocked      = string(StateBlocked)
	OutcomeGone         = string(StateGone)
	OutcomeAsks         = "asks"
	OutcomeTimeout      = "timeout"
	OutcomeStopped      = "stopped"
)

// DefaultWaitPoll is wait's poll period: at most 2 Hz.
const DefaultWaitPoll = 500 * time.Millisecond

// TimeoutExit is wait's exit status when the timeout passed (as timeout(1)).
const TimeoutExit = 124

// ErrNoTimeout: every wait must be bounded (cbds: a mandatory timeout).
var ErrNoTimeout = errors.New("delegate: wait needs --timeout (a positive duration)")

// WaitOptions bounds a wait. Timeout is required; Poll is DefaultWaitPoll
// when zero.
type WaitOptions struct {
	Timeout time.Duration
	Poll    time.Duration
}

// WaitResult is wait's payload: {run, turn, outcome}, the task the turn
// answers, why (gone, blocked, stopped) and the pending asks.
type WaitResult struct {
	Run     string     `json:"run"`
	Name    string     `json:"name"`
	Turn    int        `json:"turn"`
	Outcome string     `json:"outcome"`
	Task    string     `json:"task,omitempty"`
	Detail  string     `json:"detail,omitempty"`
	Asks    []Question `json:"asks,omitempty"`
}

// ExitCode is TimeoutExit for a timeout and 0 for any settled outcome.
func (w WaitResult) ExitCode() int {
	if w.Outcome == OutcomeTimeout {
		return TimeoutExit
	}
	return 0
}

// Wait blocks until name's worker gives a durable signal or the timeout
// passes. It writes no run state (orch F-1): its only writes are the
// ledger's, under the ledger lock and only on a still-open row: a session
// the worker started after /clear joins worker_sessions, and a lost worker's
// row closes as gone with that worker's own claim episodes expired. A
// timeout changes nothing.
func Wait(ctx context.Context, d Deps, name string, o WaitOptions) (WaitResult, error) {
	if o.Timeout <= 0 {
		return WaitResult{}, ErrNoTimeout
	}
	poll := o.Poll
	if poll <= 0 {
		poll = DefaultWaitPoll
	}
	row, err := findRow(d, name)
	if err != nil {
		return WaitResult{}, err
	}
	deadline := time.Now().Add(o.Timeout)
	// Every herdr call of a look is bounded by the wait's own deadline.
	look, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		res, settled := waitOnce(look, d, row.ID, name)
		if err := ctx.Err(); err != nil {
			return res, err // the caller cancelled: never a timeout
		}
		if look.Err() != nil {
			settled = false // the deadline cut the look short: it is a timeout
		}
		if settled {
			return res, nil
		}
		left := time.Until(deadline)
		if left <= 0 || look.Err() != nil {
			res.Outcome = OutcomeTimeout
			return res, nil
		}
		if err := sleepCtx(ctx, min(poll, left)); err != nil {
			return res, err
		}
	}
}

// waitOnce takes one look. A closed row settles at once: a stop always wins.
func waitOnce(ctx context.Context, d Deps, runID, name string) (WaitResult, bool) {
	res := WaitResult{Run: runID, Name: name}
	r, ok, err := d.Ledger.Get(runID)
	switch {
	case err != nil:
		return res, false // unreadable for now; look again
	case !ok:
		res.Outcome, res.Detail = OutcomeGone, "the run has no ledger row"
		return res, true
	case !r.IsOpen():
		res.Outcome, res.Detail = OutcomeStopped, "end_reason="+r.EndReason
		if r.EndReason == delegationledger.EndGone {
			res.Outcome = OutcomeGone
		}
		return res, true
	}
	rf, info, err := loadInfo(d.Store, runID)
	if err != nil {
		res.Outcome, res.Detail = OutcomeGone, err.Error()
		return res, true
	}
	// The ack is read before the events: the hook writes its prompt event
	// before the ack, so a seen ack's turn is in the snapshot, and an ack
	// written during the look reads as unacked until the next one.
	t, hasTask := newestTask(d.Store, name, runID)
	o, f := observe(ctx, d, rf, info)
	addSessions(d, runID, f.sessions)
	if hasTask {
		res.Task, res.Turn = t.msg.ID, t.turn
	}
	return settle(d, res, o, f, hasTask && !t.acked)
}

// settle is wait's rule over the one readiness predicate. Durable signals
// come first: a lost pane or an exited harness, then the current task's result file, which ends
// the wait even with silent hooks or a turn stuck at working (D1, D1b).
// A folder-trust dialog is reported next. While the newest task is not yet
// acked, the hook turn state belongs to an older turn and is rejected. Input-line text is never read (D8).
func settle(d Deps, res WaitResult, o Observation, f eventFacts, unacked bool) (WaitResult, bool) {
	switch {
	case o.PaneGone || o.Exited: // a lost pane or an exited harness: worker loss
		closeLostRun(d, res.Run, f.sessions)
		res.Outcome, res.Detail = OutcomeGone, o.GoneWhy
	case o.TaskAnswered:
		res.Outcome = OutcomeDone
	case o.Question:
		res.Outcome, res.Asks = OutcomeAsks, pendingQuestions(d.Store, res.Name, res.Run, f)
	case o.Dialog != "": // a trust dialog holds the first task unacked
		res.Outcome, res.Detail = OutcomeBlocked, Ready(o).Detail
	case o.TaskPending && unacked: // only stale turn signals are rejected
		return res, false
	default:
		s, ok := WaitOutcome(o)
		if !ok {
			return res, false
		}
		res.Outcome, res.Detail = string(s), Ready(o).Detail
	}
	return res, true
}

// addSessions adds the bound sessions to an open row's worker_sessions, so
// the claim gate keeps a /clear session as family. A closed row is left.
func addSessions(d Deps, runID string, sessions []string) {
	if len(sessions) > 0 {
		_, _ = d.Ledger.EnrichIfOpen(runID, delegationledger.Enrichment{WorkerSessions: sessions})
	}
}

// closeLostRun is wait's worker-loss close: it reads the row and run folder
// and hands them to closeLost, adding the sessions this look saw bound.
func closeLostRun(d Deps, runID string, sessions []string) {
	r, ok, err := d.Ledger.Get(runID)
	if err != nil || !ok {
		return
	}
	r.WorkerSessions = mergeSessions(r.WorkerSessions, sessions)
	rf, _ := d.Store.Run(runID)
	closeLost(d, r, rf)
}
