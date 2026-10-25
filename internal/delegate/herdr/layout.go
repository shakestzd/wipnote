package herdr

import (
	"context"
	"errors"
	"fmt"
)

// Two layouts share this file.
//
// One-shot runs keep the codex-herdr-pane tile (TileTarget): by n live
// delegation panes of the caller, n=0 splits the caller's pane right, n=1
// splits pane 1 down, n=2 pane 1 right, n=3 pane 2 right (a 2x2 grid beside
// the caller), n>=4 the newest pane down; at the cap the run gets no pane.
//
// Interactive workers share ONE workers tab per caller, in the caller's
// workspace (Place). The first worker creates the tab and uses its root pane;
// the 2nd splits the first pane right, the 3rd the first pane down, the 4th
// the second pane down, the 5th and 6th the largest pane. The caller's own tab
// is never split, and at most MaxWorkers workers live in the tab.

// MaxWorkers caps the live workers in one workers tab.
const MaxWorkers = 6

// ErrWorkersCap refuses a worker beyond MaxWorkers.
var ErrWorkersCap = errors.New("herdr: workers tab is full")

// TileTarget picks the split for the next one-shot pane given the caller's
// live delegation panes in creation order. An empty target means the caller's
// pane (--current). ok is false at the cap; a cap below 1 counts as 1.
func TileTarget(live []PaneID, maxPanes int) (target PaneID, dir Direction, ok bool) {
	n := len(live)
	if n >= max(1, maxPanes) {
		return "", "", false
	}
	switch n {
	case 0:
		return "", Right, true
	case 1:
		return live[0], Down, true
	case 2:
		return live[0], Right, true
	case 3:
		return live[1], Right, true
	}
	return live[n-1], Down, true
}

// Placement is where the next worker goes: a new workers tab, or a split.
type Placement struct {
	NewTab bool
	Target PaneID
	Dir    Direction
}

// wideRatio: a cell is about twice as tall as wide, so a pane at least twice
// as many columns as rows is split right.
const wideRatio = 2

// WorkerPlacement picks the next worker's place from the live worker panes of
// the tab in spawn order. sizes is only used from the 5th worker on; without
// sizes the newest pane is halved downwards.
func WorkerPlacement(live []PaneID, sizes []PaneRect) (Placement, error) {
	switch n := len(live); {
	case n >= MaxWorkers:
		return Placement{}, fmt.Errorf("%w: %d live workers (cap %d)", ErrWorkersCap, n, MaxWorkers)
	case n == 0:
		return Placement{NewTab: true}, nil
	case n == 1:
		return Placement{Target: live[0], Dir: Right}, nil
	case n == 2:
		return Placement{Target: live[0], Dir: Down}, nil
	case n == 3:
		return Placement{Target: live[1], Dir: Down}, nil
	}
	return largest(live, sizes), nil
}

func largest(live []PaneID, sizes []PaneRect) Placement {
	inTab := map[PaneID]bool{}
	for _, p := range live {
		inTab[p] = true
	}
	var best *PaneRect
	for i := range sizes {
		r := &sizes[i]
		if inTab[r.Pane] && (best == nil || r.Width*r.Height > best.Width*best.Height) {
			best = r
		}
	}
	if best == nil {
		return Placement{Target: live[len(live)-1], Dir: Down}
	}
	if best.Width >= wideRatio*best.Height {
		return Placement{Target: best.Pane, Dir: Right}
	}
	return Placement{Target: best.Pane, Dir: Down}
}

// WorkersTab is the one tab delegate created for a caller's workers. Only a
// tab recorded here is ever closed.
type WorkersTab struct {
	ID        TabID
	Workspace string
	Label     string
}

// Place gives the next worker a pane. live is the caller's recorded worker
// panes in spawn order; a pane herdr says is gone is dropped, and any other
// lookup failure is returned, so a flaky query never frees capacity. With no
// tab, or no pane left in it, a new workers tab is made in the caller's
// workspace. It returns the tab record (new or unchanged) and the pane.
func Place(ctx context.Context, c Client, tab *WorkersTab, live []PaneID, label, cwd string) (*WorkersTab, PaneID, error) {
	var existing []PaneID
	for _, p := range live {
		_, err := c.Get(ctx, p)
		if IsAbsent(err) {
			continue
		}
		if err != nil {
			return tab, "", err
		}
		existing = append(existing, p)
	}
	if tab == nil || len(existing) == 0 {
		ws, err := c.CallerWorkspace(ctx)
		if err != nil {
			return tab, "", err
		}
		id, root, err := c.TabCreate(ctx, ws, label, cwd)
		if err != nil {
			return tab, "", err
		}
		return &WorkersTab{ID: id, Workspace: ws, Label: label}, root, nil
	}
	var sizes []PaneRect
	if len(existing) >= 4 && len(existing) < MaxWorkers {
		sizes, _ = c.Layout(ctx, existing[0]) // unknown sizes: halve the newest pane
	}
	at, err := WorkerPlacement(existing, sizes)
	if err != nil {
		return tab, "", err
	}
	pane, err := c.Split(ctx, at.Target, at.Dir, cwd)
	return tab, pane, err
}

// ReleaseTab closes the recorded workers tab once no live worker uses it. A
// tab herdr says is gone counts as closed. It reports whether the record can
// be dropped.
func ReleaseTab(ctx context.Context, c Client, tab *WorkersTab, live []PaneID) (bool, error) {
	if tab == nil || len(live) > 0 {
		return false, nil
	}
	if err := c.TabClose(ctx, tab.ID); err != nil && !IsAbsent(err) {
		return false, err
	}
	return true, nil
}
