package session

import (
	"context"
	"fmt"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Worker lifecycle (feat-f1171e59). A worker's pane is its own only while it
// holds the terminal pinned at spawn: every key typed and every close checks
// that first (ownPane), so a pane id herdr gave to someone else is never
// touched. A lost worker (workerLost) leaves the caller's workers record by
// run; an exited harness's own shell-only pane is closed and the tab released
// after the last worker, as stop does; a gone or reused pane is left alone.

// lostWorker is a worker checkLive found lost, with what showed it.
type lostWorker struct {
	info Info
	ob   Observation
}

// ownPane reports whether the worker's pane still holds its pinned terminal.
// An unpinned terminal, an absent pane or a failed lookup is not its own; the
// string says why.
func ownPane(ctx context.Context, c herdr.Client, info Info) (bool, string) {
	pane := herdr.PaneID(info.Pane)
	if info.TerminalID == "" {
		return false, fmt.Sprintf("worker %s's terminal id is not known, so pane %s may not be its own", info.Name, pane)
	}
	p, err := c.Get(ctx, pane)
	switch {
	case herdr.IsAbsent(err):
		return false, fmt.Sprintf("pane %s is gone", pane)
	case err != nil:
		return false, fmt.Sprintf("herdr cannot say what pane %s holds: %v", pane, err)
	case p.TerminalID != info.TerminalID:
		return false, fmt.Sprintf("pane %s now holds terminal %s, not the worker's %s", pane, p.TerminalID, info.TerminalID)
	}
	return true, ""
}

// closeOwnPane closes the worker's pane only while it holds its terminal.
func closeOwnPane(ctx context.Context, c herdr.Client, info Info) bool {
	if c == nil || info.Pane == "" {
		return false
	}
	if ok, _ := ownPane(ctx, c, info); !ok {
		return false
	}
	return c.Close(ctx, herdr.PaneID(info.Pane)) == nil
}

// loseWorker is the worker-loss path (workerLost): no worker process is left
// under herdr and no SessionEnd will fire. The run leaves the caller's
// workers record (unplace), then closeLost closes it. The caller's episode is
// never touched.
func loseWorker(ctx context.Context, d Deps, r delegationledger.Record, rf *runstore.RunFolder, info Info, ob Observation) {
	if info.Run != "" {
		_ = withWorkers(d.Store, info.CallerPane, func(w *Workers) error {
			unplace(ctx, d, w, info, ob)
			return nil
		})
	}
	closeLost(d, r, rf)
}

// unplace removes a lost worker from w. An exited harness's own shell-only
// pane is closed and the workers tab released once no worker is left; a gone
// or reused pane is only dropped from the record, and herdr is not touched.
func unplace(ctx context.Context, d Deps, w *Workers, info Info, ob Observation) {
	w.dropRun(info.Run)
	if !ob.Exited || ob.Reused || !closeOwnPane(ctx, d.Herdr, info) {
		return
	}
	if closed, err := release(ctx, d.Herdr, w, herdr.PaneID(info.Pane)); err == nil && closed {
		w.Tab = nil
	}
}

// sweepLost runs under the caller's workers lock before the cap is checked:
// each recorded worker is observed, a lost one is unplaced and closed, and an
// entry whose row is no longer open is dropped. So no lost worker counts
// toward the cap or becomes a split target.
func sweepLost(ctx context.Context, d Deps, w *Workers) {
	for _, wp := range append([]WorkerPane(nil), w.Panes...) {
		r, ok, err := d.Ledger.Get(wp.Run)
		if err != nil {
			continue
		}
		if !ok || !r.IsOpen() {
			w.dropRun(wp.Run)
			continue
		}
		rf, info, err := loadInfo(d.Store, wp.Run)
		if err != nil {
			continue
		}
		if ob, _ := observe(ctx, d, rf, info); workerLost(ob) {
			unplace(ctx, d, w, info, ob)
			closeLost(d, r, rf)
		}
	}
}
