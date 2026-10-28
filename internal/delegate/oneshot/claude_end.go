package oneshot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// end ends the worker on wipnote's verdict: the exit command (escape first
// when a turn may run), then SIGTERM to the pane's own processes when the
// agent did not exit. A worker with no session gets no typed text at all:
// SIGTERM only, and its pane closes only at a bare shell. A possible draft
// on the input line is never typed over; on a failure verdict the worker
// is still signalled, on a done one it is left to the person (default),
// and the row is closed here either way.
func (wk *worker) end(ctx context.Context, v verdict) {
	if v.cancelled { // the caller's context is done; the end still runs, bounded
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 3*wk.w.exitWait())
		defer cancel()
	}
	var exit *int
	if !v.cancelled {
		exit = &v.exit
	}
	if !wk.open() {
		return // wait closed it (the worker was lost) or someone stopped it
	}
	if !v.bound {
		wk.terminate(ctx)
	}
	so := session.StopOptions{ExitWait: wk.w.exitWait(), Poll: wk.o.poll(), Interrupt: v.running && v.bound,
		Exit: exit, NoType: !v.bound}
	res, err := session.Stop(ctx, wk.deps, wk.sp.Name, so)
	if err == nil && !res.PaneClosed && wk.open() && wk.mustSignal(v, res) {
		wk.terminate(ctx)
		so.Interrupt, so.NoType = false, true // after a signal nothing more is typed
		res, err = session.Stop(ctx, wk.deps, wk.sp.Name, so)
	}
	if err != nil || res.Why != "" {
		fmt.Fprintf(wk.o.Stderr, "wipnote: worker %s: %s\n", wk.sp.Name, firstNonEmpty(res.Why, fmt.Sprint(err)))
	}
	wk.closeRow(v, exit)
}

// mustSignal: the exit command was typed and ignored, or the verdict is a
// failure (a possible draft only means nothing is typed), or the worker
// never started. A done worker behind a possible draft is left to the
// person.
func (wk *worker) mustSignal(v verdict, res session.StopResult) bool {
	return res.ExitTyped || v.exit != 0 || !v.bound
}

func (wk *worker) open() bool {
	r, ok, err := wk.o.Ledger.Get(wk.sp.Run)
	return err == nil && ok && r.IsOpen()
}

// terminate sends SIGTERM to the processes of the worker's own pane (never
// its shell, never a pid now held by another process), then waits for the
// pane to hold its shell alone.
func (wk *worker) terminate(ctx context.Context) {
	procs, err := session.WorkerProcs(ctx, wk.deps, wk.sp.Run)
	if err != nil {
		fmt.Fprintf(wk.o.Stderr, "wipnote: worker %s was not signalled: %v\n", wk.sp.Name, err)
		return
	}
	for _, p := range procs {
		if session.SameProc(wk.deps, p) {
			_ = wk.w.signal(p.PID)
		}
	}
	deadline := time.Now().Add(wk.w.exitWait())
	for time.Now().Before(deadline) && ctx.Err() == nil {
		if left, err := session.WorkerProcs(ctx, wk.deps, wk.sp.Run); err != nil || len(left) == 0 {
			return
		}
		time.Sleep(wk.o.poll())
	}
}

// closeRow closes a row stop could not (the pane stayed open), with the
// verdict's exit, and finishes the run folder; a row already closed is kept.
func (wk *worker) closeRow(v verdict, exit *int) {
	reason := delegationledger.EndExit
	if exit == nil {
		reason = delegationledger.EndStop
	}
	closed, err := wk.o.Ledger.CloseIfOpen(wk.sp.Run, delegationledger.CloseInfo{Ended: time.Now().UTC(), Exit: exit, Reason: reason})
	if err == nil && closed {
		_ = wk.rf.Finish(v.exit, time.Now().UTC())
	}
}

// report writes the run folder's answer and log, renders the page and
// prints the claude -p report; the row gets the usage and the session.
func (wk *worker) report(r Recipe, v verdict) Result {
	if v.exit == 0 && v.answer != "" {
		_ = wk.rf.WriteFile(runstore.FileLastMD, []byte(strings.TrimRight(v.answer, "\n")+"\n"))
	}
	log := wk.logText(v)
	_ = wk.rf.WriteFile(runstore.FileCleanLog, []byte(log))
	_ = wk.rf.WriteFile(runstore.FileCleanMD, []byte(log))
	_ = wk.rf.WriteFile(runstore.FileRawLog, []byte(log))
	if _, fed := readStatus(wk.rf); !fed {
		_ = wk.rf.WriteFile(runstore.FileUsage, nil)
	}
	page, _ := render.WritePage(wk.rf)
	printReport(wk.o, r, wk.rf, page)
	enrichRow(wk.o.Ledger, wk.rf, wk.sp.Run)
	res := Result{RunID: wk.sp.Run, Exit: v.exit, EndReason: delegationledger.EndExit}
	if v.cancelled {
		res.EndReason = delegationledger.EndStop
	}
	return res
}

// logText is the run's log: where the worker ran and how it ended. A
// failure's last line names the reason, so the report's tail shows it.
func (wk *worker) logText(v verdict) string {
	var b strings.Builder
	fmt.Fprintf(&b, "interactive claude worker %s in pane %s, session %s\n", wk.sp.Name, wk.sp.Pane, wk.info.SessionID)
	fmt.Fprintf(&b, "transcript %s\n", wk.info.Transcript)
	if v.reason == "" {
		b.WriteString("done: the result file was written\n")
	} else {
		fmt.Fprintf(&b, "failed: %s: %s\n", v.reason, v.detail)
	}
	return b.String()
}
