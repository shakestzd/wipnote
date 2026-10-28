package session

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// DefaultExitWait bounds how long stop waits for the agent to exit.
const DefaultExitWait = 20 * time.Second

// StopOptions bounds a stop.
type StopOptions struct {
	ExitWait time.Duration // DefaultExitWait when zero
	Poll     time.Duration // DefaultPoll when zero
}

// Survivor is a recorded worker process still running after its pane closed.
type Survivor struct {
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// StopResult is stop's payload (D5): what it closed and, when the pane
// stays open, always why.
type StopResult struct {
	Run        string     `json:"run"`
	Name       string     `json:"name"`
	PaneClosed bool       `json:"pane_closed"`
	TabClosed  bool       `json:"tab_closed"`
	Survivors  []Survivor `json:"survivors"`
	Why        string     `json:"why,omitempty"`
}

type recorded struct {
	Survivor
	start string
}

// exitCommand is what each harness takes to quit at an empty input line.
var exitCommand = map[string]string{HarnessClaude: "/exit", HarnessCodex: "/quit"}

// Stop is the verified stop (orch shutdown.stop, finding 2). It records the
// worker's pids with their start times first (process-info is gone once the
// pane closes), asks the agent to exit only at an input line with no draft,
// waits until process-info POSITIVELY shows the shell alone, then closes the
// pane and checks each recorded pid. It never signals or kills.
func Stop(ctx context.Context, d Deps, name string, o StopOptions) (StopResult, error) {
	r, rf, info, err := findWorker(d, name)
	if err != nil {
		return StopResult{}, err
	}
	res := StopResult{Run: r.ID, Name: name, Survivors: []Survivor{}}
	if d.Herdr == nil {
		return res, ErrNotInHerdr
	}
	pane := herdr.PaneID(info.Pane)
	p, err := d.Herdr.Get(ctx, pane)
	if herdr.IsAbsent(err) {
		res.Why = fmt.Sprintf("pane %s was already gone; the run is closed as gone and its claims as expired", pane)
		finishStop(ctx, d, rf, info, &res, delegationledger.EndGone)
		expireWorkerClaims(d, r, rf)
		return res, nil
	}
	if err == nil && reused(info, p) {
		res.Why = fmt.Sprintf("pane %s now holds terminal %s, not the worker's %s; nothing was typed or closed, and the run is closed as gone and its claims as expired",
			pane, p.TerminalID, info.TerminalID)
		forgetWorker(d, rf, info)
		expireWorkerClaims(d, r, rf)
		return res, nil
	}
	procs, why := exitAgent(ctx, d, info, o)
	if why != "" {
		res.Why = why
		return res, nil
	}
	if err := d.Herdr.Close(ctx, pane); err != nil {
		res.Why = fmt.Sprintf("herdr would not close pane %s: %v", pane, err)
		return res, nil
	}
	res.PaneClosed = true
	for _, p := range procs {
		if start, ok := d.procStart(p.PID); ok && start == p.start {
			res.Survivors = append(res.Survivors, p.Survivor)
		}
	}
	finishStop(ctx, d, rf, info, &res, delegationledger.EndStop)
	return res, nil
}

// exitAgent records the pids, asks the agent to exit and waits for a bare
// shell. A non-empty why means the pane must stay open; nothing was typed
// unless the why says so.
func exitAgent(ctx context.Context, d Deps, info Info, o StopOptions) ([]recorded, string) {
	pane := herdr.PaneID(info.Pane)
	pi, err := d.Herdr.Processes(ctx, pane)
	if err != nil {
		return nil, fmt.Sprintf("herdr process-info failed for pane %s, so the worker's pids are unknown; nothing was typed: %v", pane, err)
	}
	procs := record(d, pi)
	if bareShell(pi) {
		return procs, ""
	}
	if !pi.Known {
		return procs, fmt.Sprintf("herdr process-info for pane %s has no shell pid or process list (unknown is not a bare shell); nothing was typed", pane)
	}
	if in := ReadInput(ctx, d.Herdr, pane); in.Draft() {
		return procs, "a draft may be on the worker's input line (" + in.why() + "); nothing was typed, so a person's text is not lost"
	}
	if err := d.Herdr.SendText(ctx, pane, exitCommand[info.Harness]); err != nil {
		return procs, fmt.Sprintf("herdr would not type the exit command: %v", err)
	}
	if err := d.Herdr.SendKeys(ctx, pane, "enter"); err != nil {
		return procs, fmt.Sprintf("herdr would not press enter after the exit command: %v", err)
	}
	return procs, waitBareShell(ctx, d, pane, o)
}

func waitBareShell(ctx context.Context, d Deps, pane herdr.PaneID, o StopOptions) string {
	wait, poll := o.ExitWait, o.Poll
	if wait <= 0 {
		wait = DefaultExitWait
	}
	if poll <= 0 {
		poll = DefaultPoll
	}
	deadline := time.Now().Add(wait)
	var last herdr.ProcessInfo
	for time.Now().Before(deadline) {
		if pi, err := d.Herdr.Processes(ctx, pane); err == nil {
			if bareShell(pi) {
				return ""
			}
			last = pi
		}
		if sleepCtx(ctx, poll) != nil {
			return "interrupted while waiting for the agent to exit; the pane stays open"
		}
	}
	return fmt.Sprintf("the agent did not exit within %s; pane %s still runs %s", wait, pane, procNames(last))
}

// record keeps every foreground process but the shell, with its start time.
func record(d Deps, pi herdr.ProcessInfo) []recorded {
	var out []recorded
	for _, p := range pi.Foreground {
		if p.PID == pi.ShellPID || p.PID <= 0 {
			continue
		}
		start, _ := d.procStart(p.PID)
		out = append(out, recorded{Survivor{PID: p.PID, Name: p.Name}, start})
	}
	return out
}

func procNames(pi herdr.ProcessInfo) string {
	if !pi.Known {
		return "unknown processes"
	}
	var names []string
	for _, p := range pi.Foreground {
		names = append(names, fmt.Sprintf("%s (%d)", p.Name, p.PID))
	}
	return strings.Join(names, ", ")
}

// finishStop drops the pane from the workers tab (closing the tab after the
// last worker), closes the ledger row and finishes the run folder.
func finishStop(ctx context.Context, d Deps, rf *runstore.RunFolder, info Info, res *StopResult, reason string) {
	_ = withWorkers(d.Store, info.CallerPane, func(w *Workers) error {
		closed, err := release(ctx, d.Herdr, w, herdr.PaneID(info.Pane))
		res.TabClosed = closed
		if err != nil && res.Why == "" {
			res.Why = "the workers tab could not be checked: " + err.Error()
		}
		return nil
	})
	_, _ = d.Ledger.CloseIfOpen(info.Run, delegationledger.CloseInfo{Ended: d.now(), Reason: reason})
	_ = rf.Finish(0, d.now())
}

// reused reports that a pinned worker's pane id now holds another terminal:
// herdr gave the id to someone else, so the pane is not the worker's.
func reused(info Info, p herdr.Pane) bool {
	return info.TerminalID != "" && p.TerminalID != "" && p.TerminalID != info.TerminalID
}

// forgetWorker closes a worker whose pane id was reused without touching
// herdr: the pane leaves the caller's record (its tab is left alone, it now
// holds another session), the row closes as gone, the run folder finishes.
func forgetWorker(d Deps, rf *runstore.RunFolder, info Info) {
	_ = withWorkers(d.Store, info.CallerPane, func(w *Workers) error {
		var keep []WorkerPane
		for _, wp := range w.Panes {
			if wp.Pane != herdr.PaneID(info.Pane) {
				keep = append(keep, wp)
			}
		}
		w.Panes = keep
		return nil
	})
	_, _ = d.Ledger.CloseIfOpen(info.Run, delegationledger.CloseInfo{Ended: d.now(), Reason: delegationledger.EndGone})
	_ = rf.Finish(1, d.now())
}

// findWorker returns the newest open interactive row named name.
func findWorker(d Deps, name string) (delegationledger.Record, *runstore.RunFolder, Info, error) {
	open, ok := d.Ledger.ReadOpen()
	if !ok {
		return delegationledger.Record{}, nil, Info{}, fmt.Errorf("delegate: the delegations ledger cannot be read")
	}
	rows := open.Interactive()
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Name == name {
			rf, info, err := loadInfo(d.Store, rows[i].ID)
			return rows[i], rf, info, err
		}
	}
	return delegationledger.Record{}, nil, Info{}, fmt.Errorf("%w: %q", ErrNoWorker, name)
}
