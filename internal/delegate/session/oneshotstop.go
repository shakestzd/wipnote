package session

import (
	"context"
	"fmt"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// endAgent ends the agent for a stop: an Interrupt stop (a Claude one-shot
// run, slice 22) presses escape first, so a running turn ends before the
// exit command is typed; then exitAgent asks the agent to exit.
func endAgent(ctx context.Context, d Deps, info Info, o StopOptions) ([]recorded, string, bool) {
	if o.Interrupt {
		interruptTurn(ctx, d, info, o)
	}
	return exitAgent(ctx, d, info, o)
}

// interruptTurn presses escape once in the worker's own pane and gives the
// harness a poll to settle.
func interruptTurn(ctx context.Context, d Deps, info Info, o StopOptions) {
	if ok, _ := ownPane(ctx, d.Herdr, info); !ok {
		return
	}
	if d.Herdr.SendKeys(ctx, herdr.PaneID(info.Pane), "escape") == nil {
		poll := o.Poll
		if poll <= 0 {
			poll = DefaultPoll
		}
		_ = sleepCtx(ctx, poll)
	}
}

// Proc is one of a worker's foreground processes with its start time.
type Proc struct {
	PID   int
	Name  string
	Start string
}

// WorkerProcs names the processes in run's own pane other than its shell,
// each with its start time, for a one-shot run that ends its worker itself
// (slice 22). It reads only: the pane must still hold the worker's pinned
// terminal (ownPane), and an unknown process list is an error, never empty.
func WorkerProcs(ctx context.Context, d Deps, run string) ([]Proc, error) {
	_, info, err := loadInfo(d.Store, run)
	if err != nil {
		return nil, err
	}
	if d.Herdr == nil {
		return nil, ErrNotInHerdr
	}
	if ok, why := ownPane(ctx, d.Herdr, info); !ok {
		return nil, fmt.Errorf("delegate: %s", why)
	}
	pi, err := d.Herdr.Processes(ctx, herdr.PaneID(info.Pane))
	if err != nil || !pi.Known {
		return nil, fmt.Errorf("delegate: herdr cannot list pane %s's processes (%v)", info.Pane, err)
	}
	var out []Proc
	for _, p := range record(d, pi) {
		out = append(out, Proc{PID: p.PID, Name: p.Name, Start: p.start})
	}
	return out, nil
}

// SameProc reports that p still runs with the start time recorded for it,
// so a pid the system gave to another process never counts as the worker's.
func SameProc(d Deps, p Proc) bool {
	start, ok := d.procStart(p.PID)
	return ok && p.Start != "" && start == p.Start
}
