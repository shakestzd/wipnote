package oneshot

import (
	"context"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// verdict is wipnote's decision on a worker run: done (reason empty) or
// failed with a named reason (the claude -p subtypes where one exists).
type verdict struct {
	exit      int
	reason    string
	detail    string
	answer    string // the result file's body, when done
	bound     bool   // a session_start was seen: the exit command may be typed
	running   bool   // a turn may still run: interrupt it before /exit
	cancelled bool   // the caller stopped the run
}

// Named failure reasons.
const (
	ReasonNoResult      = "no_result"
	ReasonStopFailure   = "stop_failure"
	ReasonGone          = "worker_gone"
	ReasonStopped       = "stopped"
	ReasonStartTimeout  = "session_start_timeout"
	ReasonPluginNotLoad = "plugin_not_loaded"
	ReasonNoPrompt      = "prompt_not_submitted"
	ReasonMaxTurns      = "error_max_turns"
	ReasonMaxBudget     = "error_max_budget_usd"
	ReasonInterrupted   = "interrupted"
	ReasonWaitFailed    = "wait_failed"
	exitInterrupted     = 130
	noteAsksOnce        = "asks"
	noteBlockedOnce     = "blocked"
)

// eventView is what the run's hook events say for the verdict.
type eventView struct {
	bound      bool
	running    bool   // a prompt with no stop or error after it
	failure    string // the error event after the last prompt
	sessionEnd string // the last session_end reason
	prompted   bool
	ended      bool
}

// Only the worker's own session counts: the pinned --session-id, and the
// session that follows it after /clear (its SessionEnd says "clear", then a
// new session starts). Any other session writing into the run folder (a
// nested claude run from a tool call inherits the run env) is ignored.
func viewEvents(evs []runstore.Event, own string) eventView {
	var v eventView
	owned, cleared := map[string]bool{own: true}, false
	for _, ev := range evs {
		if !owned[ev.SID] {
			if cleared && ev.Kind == runstore.KindSessionStart {
				owned[ev.SID], cleared = true, false
			} else {
				continue
			}
		}
		v.take(ev)
		if ev.Kind == runstore.KindSessionEnd && ev.Summary == "clear" {
			cleared = true
		}
	}
	return v
}

func (v *eventView) take(ev runstore.Event) {
	switch ev.Kind {
	case runstore.KindSessionStart:
		v.bound = true
	case runstore.KindPrompt:
		v.prompted, v.running, v.ended, v.failure = true, true, false, ""
	case runstore.KindStop:
		v.running, v.ended = false, v.prompted
	case runstore.KindError:
		v.running, v.failure = false, firstNonEmpty(ev.Summary, "unknown")
	case runstore.KindSessionEnd:
		v.sessionEnd = firstNonEmpty(ev.Summary, "unknown")
	}
}

// watch waits on the worker (session.Wait, one poll at a time) until the
// verdict: its result file, a failure its hooks recorded, its loss, or a
// cap or startup bound wipnote enforces.
func (wk *worker) watch(ctx context.Context) verdict {
	start := time.Now()
	turns := &turnCounter{path: wk.info.Transcript}
	noted := map[string]bool{}
	for {
		res, err := session.Wait(ctx, wk.deps, wk.sp.Name, session.WaitOptions{Timeout: wk.o.poll(), Poll: wk.o.poll()})
		evs, _ := wk.rf.ReadEvents()
		ev := viewEvents(evs, wk.info.SessionID)
		base := verdict{exit: 1, bound: ev.bound, running: ev.running}
		if ctx.Err() != nil {
			base.exit, base.reason, base.cancelled = exitInterrupted, ReasonInterrupted, true
			return base
		}
		if err != nil {
			base.reason, base.detail = ReasonWaitFailed, err.Error()
			return base
		}
		if v, done := wk.judge(res, ev, base, time.Since(start), turns); done {
			return v
		}
		wk.note(res, noted)
	}
}

// judge applies the rules in order: the result file wins, then what the
// hooks and the wait say, then the bounds wipnote enforces.
func (wk *worker) judge(res session.WaitResult, ev eventView, v verdict, took time.Duration, turns *turnCounter) (verdict, bool) {
	switch {
	case res.Outcome == session.OutcomeDone || wk.answered():
		return wk.done(v)
	case ev.failure != "":
		v.reason, v.detail = ReasonStopFailure, "the turn ended on an API error: "+ev.failure
	case ev.ended: // the worker's own turn ended and no result file was found above
		v.reason, v.detail = ReasonNoResult, "the turn ended without writing the result file"
	case res.Outcome == session.OutcomeGone:
		v.reason, v.detail = ReasonGone, goneDetail(res, ev)
	case res.Outcome == session.OutcomeStopped:
		v.reason, v.detail = ReasonStopped, "the run was stopped ("+res.Detail+")"
	default:
		return wk.bounds(res, ev, v, took, turns)
	}
	return v, true
}

// answered reports that the brief's result file exists.
func (wk *worker) answered() bool {
	mb, err := wk.o.Store.Mailbox(wk.info.Name)
	if err != nil {
		return false
	}
	_, ok, _ := mb.Result(wk.info.Task)
	return ok
}

func (wk *worker) done(v verdict) (verdict, bool) {
	v.exit = 0
	if mb, err := wk.o.Store.Mailbox(wk.info.Name); err == nil {
		if msg, ok, _ := mb.Result(wk.info.Task); ok {
			v.answer = msg.Body
		}
	}
	return v, true
}

func goneDetail(res session.WaitResult, ev eventView) string {
	if ev.sessionEnd != "" {
		return "the session ended (" + ev.sessionEnd + ") before the result file was written"
	}
	return res.Detail
}

// bounds are what the interactive flags cannot enforce: the startup bound
// (SessionStart, and with it the plugin), the prompt, and the caps.
func (wk *worker) bounds(res session.WaitResult, ev eventView, v verdict, took time.Duration, turns *turnCounter) (verdict, bool) {
	limit := wk.w.startTimeout()
	st, fed := readStatus(wk.rf)
	switch {
	case !ev.bound && took > limit && fed:
		v.reason, v.detail = ReasonPluginNotLoad, fmt.Sprintf("claude is up (its status line ran) but the wipnote plugin's SessionStart did not fire within %s", limit)
	case !ev.bound && took > limit:
		v.reason, v.detail = ReasonStartTimeout, fmt.Sprintf("no SessionStart from the worker within %s", limit)
	case ev.bound && !ev.prompted && took > 2*limit:
		v.reason, v.detail = ReasonNoPrompt, fmt.Sprintf("the session started but the brief was not submitted within %s", 2*limit)
	case wk.plan.MaxBudget > 0 && st.CostUSD >= wk.plan.MaxBudget:
		v.reason, v.detail = ReasonMaxBudget, fmt.Sprintf("cost $%s reached --max-budget-usd %s", num(st.CostUSD), num(wk.plan.MaxBudget))
	case wk.plan.MaxTurns > 0 && wk.info.Transcript != "" && turns.count() > wk.plan.MaxTurns:
		v.reason, v.detail = ReasonMaxTurns, fmt.Sprintf("more than --max-turns %d turns", wk.plan.MaxTurns)
	default:
		return v, false
	}
	return v, true
}

// note tells the caller once that the worker waits on a person.
func (wk *worker) note(res session.WaitResult, noted map[string]bool) {
	key := ""
	switch res.Outcome {
	case session.OutcomeAsks:
		key = noteAsksOnce
	case session.OutcomeBlocked:
		key = noteBlockedOnce
	}
	if key == "" || noted[key] {
		return
	}
	noted[key] = true
	fmt.Fprintf(wk.o.Stderr, "wipnote: worker %s %s; answer it in pane %s (the run waits)\n", wk.sp.Name, firstNonEmpty(res.Detail, "asks a question"), wk.sp.Pane)
}
