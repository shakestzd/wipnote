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
	// A Claude one-shot run's end (slice 22), which wipnote decides itself:
	// Interrupt presses escape before the exit command, so a running turn
	// ends first, and Exit closes the row as exit with that status instead
	// of stop. Stop still never signals (D5): the one-shot run does that
	// itself, to the pids WorkerProcs names, when the exit command did not
	// end the agent.
	// NoType types nothing: the pane is closed only at a bare shell. For a
	// worker whose session never started, where the pane may show a
	// dialog that a typed Enter would answer.
	Interrupt bool
	Exit      *int
	NoType    bool
}

// Survivor is a recorded worker process still running after its pane closed.
type Survivor struct {
	PID   int    `json:"pid"`
	Name  string `json:"name"`
	Where string `json:"where,omitempty"`
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
	Unchecked  string     `json:"unchecked,omitempty"`
	// Ended is the row's end reason as it stands after the stop: stop's own,
	// or the one a concurrent close (list or wait saw the worker gone) wrote.
	Ended string `json:"ended,omitempty"`
	// ExitTyped: the exit command was typed and the agent did not exit in
	// time (the pane stays open). Only then may a one-shot run signal it.
	ExitTyped bool `json:"exit_typed,omitempty"`
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
	ob, _ := observe(ctx, d, rf, info)
	if workerLost(ob) && ob.PaneGone {
		stopUnreachable(ctx, d, r, rf, info, ob, &res)
		return res, nil
	}
	if ok, why := ownPane(ctx, d.Herdr, info); !ok {
		res.Why = why + "; nothing was typed or closed"
		return res, nil
	}
	inside := recordInside(ctx, d, info, &res)
	procs, why, typed := endAgent(ctx, d, info, o)
	if why != "" {
		res.Why, res.ExitTyped = why, typed
		return res, nil
	}
	if !closeOwnPane(ctx, d.Herdr, info) {
		res.Why = fmt.Sprintf("pane %s was not closed: herdr refused, or it no longer holds the worker's terminal %s", info.Pane, info.TerminalID)
		return res, nil
	}
	res.PaneClosed = true
	res.Survivors = survivors(d, info, procs)
	checkInside(ctx, d, info, inside, &res)
	endStop(ctx, d, r, rf, info, ob, &res, o.Exit)
	return res, nil
}

// survivors are the recorded pids still running with their start times.
func survivors(d Deps, info Info, procs []recorded) []Survivor {
	out := []Survivor{}
	for _, p := range procs {
		if start, ok := d.procStart(p.PID); ok && start == p.start {
			if info.Place == PlaceContainer {
				p.Where = PlaceHost
			}
			out = append(out, p.Survivor)
		}
	}
	return out
}

// endStop ends a stopped worker's run: as stop, or as gone (its claims
// expired) when its harness had already exited, as every loss does.
func endStop(ctx context.Context, d Deps, r delegationledger.Record, rf *runstore.RunFolder, info Info, ob Observation, res *StopResult, exit *int) {
	reason := delegationledger.EndStop
	if workerLost(ob) {
		reason = delegationledger.EndGone
	}
	if exit != nil {
		// A one-shot run's own verdict, also when its signal is what ended
		// the harness before this look.
		reason = delegationledger.EndExit
	}
	if finishStop(ctx, d, rf, info, res, reason, exit) && workerLost(ob) {
		if exit == nil {
			res.Why = ob.GoneWhy + "; the pane was closed, and the run is closed as gone and its claims as expired"
		}
		expireWorkerClaims(d, r, rf)
	}
}

// stopUnreachable is stop of a lost worker whose pane is not its own any
// more: gone, or its id given to another terminal. Nothing is typed or
// closed in herdr; the run is closed as gone and its claims as expired.
func stopUnreachable(ctx context.Context, d Deps, r delegationledger.Record, rf *runstore.RunFolder, info Info, ob Observation, res *StopResult) {
	if ob.Reused {
		res.Why = ob.GoneWhy + " (the worker's pinned terminal); nothing was typed or closed, and the run is closed as gone and its claims as expired"
		loseWorker(ctx, d, r, rf, info, ob)
		res.Ended = endedOf(d, r.ID)
		return
	}
	res.Why = ob.GoneWhy + " already; the run is closed as gone and its claims as expired"
	if finishStop(ctx, d, rf, info, res, delegationledger.EndGone, nil) {
		expireWorkerClaims(d, r, rf)
	}
}

// exitAgent records the pids, asks the agent to exit and waits for a bare
// shell. A non-empty why means the pane must stay open; nothing was typed
// unless the why says so.
// typed reports that the exit command was typed (the why, if any, is then
// that the agent did not exit in time).
func exitAgent(ctx context.Context, d Deps, info Info, o StopOptions) (procs []recorded, why string, typed bool) {
	pane := herdr.PaneID(info.Pane)
	pi, err := d.Herdr.Processes(ctx, pane)
	if err != nil {
		return nil, fmt.Sprintf("herdr process-info failed for pane %s, so the worker's pids are unknown; nothing was typed: %v", pane, err), false
	}
	procs = record(d, pi)
	if bareShell(pi) {
		return procs, "", false
	}
	if !pi.Known {
		return procs, fmt.Sprintf("herdr process-info for pane %s has no shell pid or process list (unknown is not a bare shell); nothing was typed", pane), false
	}
	if o.NoType {
		return procs, fmt.Sprintf("pane %s still runs %s and this stop types nothing; the pane stays open", pane, procNames(pi)), false
	}
	if in := ReadInput(ctx, d.Herdr, pane); in.Draft() {
		return procs, "a draft may be on the worker's input line (" + in.why() + "); nothing was typed, so a person's text is not lost", false
	}
	if err := d.Herdr.SendText(ctx, pane, exitCommand[info.Harness]); err != nil {
		return procs, fmt.Sprintf("herdr would not type the exit command: %v", err), false
	}
	if err := d.Herdr.SendKeys(ctx, pane, "enter"); err != nil {
		return procs, fmt.Sprintf("herdr would not press enter after the exit command: %v", err), false
	}
	return procs, waitBareShell(ctx, d, pane, o), true
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
// last worker) and closes the ledger row. Only when this call closed the row
// is the run folder finished, so a concurrent close (a list or wait that saw
// the worker gone) keeps its own end; res.Ended is the row's end as it
// stands. It reports whether this call closed the row.
func finishStop(ctx context.Context, d Deps, rf *runstore.RunFolder, info Info, res *StopResult, reason string, exit *int) bool {
	_ = withWorkers(d.Store, info.CallerPane, func(w *Workers) error {
		closed, err := release(ctx, d.Herdr, w, herdr.PaneID(info.Pane))
		res.TabClosed = closed
		if err != nil && res.Why == "" {
			res.Why = "the workers tab could not be checked: " + err.Error()
		}
		return nil
	})
	closed, err := closeWorkerRowAs(d, info.Run, reason, exit)
	if err != nil && res.Why == "" {
		res.Why = "the delegations ledger could not close the run: " + err.Error()
	}
	if err == nil && closed && rf != nil {
		code := exitOf(reason)
		if exit != nil {
			code = *exit
		}
		_ = rf.Finish(code, d.now())
	}
	res.Ended = endedOf(d, info.Run)
	return err == nil && closed
}

// exitOf is the run folder's exit for a close reason: 0 for a stop.
func exitOf(reason string) int {
	if reason == delegationledger.EndStop {
		return 0
	}
	return 1
}

// endedOf is the row's end reason as the ledger has it now.
func endedOf(d Deps, run string) string {
	if r, ok, err := d.Ledger.Get(run); err == nil && ok {
		return r.EndReason
	}
	return ""
}

// reused reports that a pinned worker's pane id now holds another terminal:
// herdr gave the id to someone else, so the pane is not the worker's.
func reused(info Info, p herdr.Pane) bool {
	return info.TerminalID != "" && p.TerminalID != "" && p.TerminalID != info.TerminalID
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
