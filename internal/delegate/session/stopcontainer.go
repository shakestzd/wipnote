package session

import (
	"context"

	"github.com/shakestzd/wipnote/internal/delegate/container"
)

// A container worker's processes are invisible to herdr, which sees only
// the host's devcontainer exec. stop records them through devcontainer exec
// before it types anything and checks them again after the pane closes, as
// it does the host's: pid and start time, never a signal.

func (d Deps) containerPids(ctx context.Context, root, run string) ([]container.Proc, error) {
	if d.ContainerPids != nil {
		return d.ContainerPids(ctx, root, run)
	}
	return container.Pids(ctx, root, run)
}

// recordInside records a container worker's pids; nil for a host worker or
// a failed probe, which is named in res.Unchecked.
func recordInside(ctx context.Context, d Deps, info Info, res *StopResult) []container.Proc {
	if info.Place != PlaceContainer {
		return nil
	}
	procs, err := d.containerPids(ctx, info.Root, info.Run)
	if err != nil {
		res.Unchecked = "the container's pids could not be recorded before the stop: " + err.Error()
		return nil
	}
	return procs
}

// checkInside reports each recorded container pid still running with the
// same start as a survivor.
func checkInside(ctx context.Context, d Deps, info Info, recorded []container.Proc, res *StopResult) {
	if info.Place != PlaceContainer || len(recorded) == 0 {
		return
	}
	now, err := d.containerPids(ctx, info.Root, info.Run)
	if err != nil {
		res.Unchecked = "the container's pids could not be checked after the stop: " + err.Error()
		return
	}
	running := map[int]string{}
	for _, p := range now {
		running[p.PID] = p.Start
	}
	for _, p := range recorded {
		if start, ok := running[p.PID]; ok && start == p.Start {
			res.Survivors = append(res.Survivors, Survivor{PID: p.PID, Name: p.Name, Where: PlaceContainer})
		}
	}
}
