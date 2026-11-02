package herdr

// Grid placement for workers in the Herdr app. Adopting a labelled terminal
// halves the last pane, so six workers end up about 50, 25, 12, 6, 3 and 3
// percent wide. The app has no balance command, but terminal.keep gives a
// running terminal a pane "right" or "down" of the focused pane, and keeps its
// name, parent, cwd and survival. So Split starts the worker unlabelled (the
// app does not adopt it), focuses the pane chosen by gridPlan, then keeps the
// terminal beside it; keep adds the tab's labels itself. Any failure falls
// back to today's adoption by labels: a spawn never fails over layout.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"time"
)

// gridPlan says where the next pane goes when the tab holds n panes: the
// 0-based creation rank of the pane to split and the direction. The sequence
// builds a 2-column grid first, then widens each row, so no pane is under a
// quarter of the width or half the height up to 8 panes. After that it cycles
// down. It assumes the tab holds only workers split by this plan; a person who
// rearranges or closes panes only makes the grid less even.
func gridPlan(n int) (rank int, dir Direction) {
	steps := [...]struct {
		rank int
		dir  Direction
	}{{0, Right}, {0, Down}, {1, Down}, {0, Right}, {2, Right}, {1, Right}, {3, Right}}
	if n >= 1 && n <= len(steps) {
		s := steps[n-1]
		return s.rank, s.dir
	}
	return n % 8, Down
}

type appTerminal struct {
	Terminal string `json:"terminal"`
	Pane     string `json:"pane"`
}

var (
	gridCall  = 2 * time.Second
	gridEvery = 100 * time.Millisecond
	gridTries = 20
)

func appOnce(ctx context.Context, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gridCall)
	defer cancel()
	return exec.CommandContext(ctx, path, args...).Output()
}

// tabTerminals lists the terminals the app shows in tab, from `herdr-app ls`.
func tabTerminals(ctx context.Context, path, tab string) ([]appTerminal, error) {
	out, err := appOnce(ctx, path, "ls")
	if err != nil {
		return nil, err
	}
	var r struct {
		Spaces []struct {
			Tabs []struct {
				ID        string        `json:"id"`
				Terminals []appTerminal `json:"terminals"`
			} `json:"tabs"`
		} `json:"spaces"`
	}
	if err := json.Unmarshal(out, &r); err != nil {
		return nil, err
	}
	for _, s := range r.Spaces {
		for _, t := range s.Tabs {
			if t.ID == tab {
				return t.Terminals, nil
			}
		}
	}
	return nil, fmt.Errorf("tab %s not shown", tab)
}

// creationOrder sorts terminals by when they started (`hc get`), dropping any
// hc cannot report.
func (c *Core) creationOrder(ctx context.Context, ts []appTerminal) []appTerminal {
	type aged struct {
		appTerminal
		at int64
	}
	var all []aged
	for _, t := range ts {
		var w struct {
			Created int64  `json:"created_ms"`
			State   string `json:"state"`
		}
		if c.call(ctx, &w, "get", t.Terminal) == nil && w.State != "finished" {
			all = append(all, aged{t, w.Created})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].at < all[j].at })
	out := make([]appTerminal, len(all))
	for i, a := range all {
		out[i] = a.appTerminal
	}
	return out
}

// splitGrid starts a worker beside the pane gridPlan picks. It runs inside
// the project's workers lock (Split is reached from placeWorker), so two spawns
// never pick the same pane. It reports ok=false, with nothing left running,
// when the grid cannot be used.
func (c *Core) splitGrid(ctx context.Context, cwd, tab string) (PaneID, bool) {
	app, err := exec.LookPath("herdr-app")
	if err != nil || tab == "" {
		return "", false
	}
	ts, err := tabTerminals(ctx, app, tab)
	if err != nil {
		return "", false
	}
	ts = c.creationOrder(ctx, ts)
	if len(ts) == 0 {
		return "", false
	}
	rank, dir := gridPlan(len(ts))
	anchor := ts[rank%len(ts)]
	var w wireTerminal
	if err := c.runNamed(ctx, &w, []string{"run", "--bg", "--outlive-parent", "--no-notify", "--cwd", cwd}); err != nil || w.Address == "" {
		return "", false
	}
	if err := keepBeside(ctx, app, anchor.Pane, w.Address, dir); err != nil {
		_ = c.call(ctx, nil, "kill", w.Address) // fall back to adoption
		_ = c.call(ctx, nil, "rm", w.Address)
		return "", false
	}
	awaitShown(ctx, app, tab, w.Address)
	return PaneID(w.Address), true
}

// awaitShown waits briefly until the app lists the terminal, because keep
// answers "pending": the next spawn counts panes from the list.
func awaitShown(ctx context.Context, app, tab, terminal string) {
	for i := 0; i < gridTries && ctx.Err() == nil; i++ {
		ts, err := tabTerminals(ctx, app, tab)
		for _, t := range ts {
			if err == nil && t.Terminal == terminal {
				return
			}
		}
		time.Sleep(gridEvery)
	}
}

// keepBeside focuses anchor and keeps terminal "dir" of it. The app learns of
// a terminal hc just started a moment later, and until then terminal.keep
// answers "no running terminal"; a single try always failed live and sent
// every worker to label adoption (50/25/6/6/6/6 percent). So focus and keep
// are retried together: keep places the pane beside the focused pane when it
// runs, so the focus must be set again each try.
func keepBeside(ctx context.Context, app, anchor, terminal string, dir Direction) error {
	var err error
	for i := 0; i < gridTries && ctx.Err() == nil; i++ {
		if _, err = appOnce(ctx, app, "do", "pane.focus", "--pane", anchor); err == nil {
			if _, err = appOnce(ctx, app, "do", "terminal.keep", "--terminal", terminal, "--place", string(dir)); err == nil {
				return nil
			}
		}
		time.Sleep(gridEvery)
	}
	if err == nil {
		err = ctx.Err()
	}
	return err
}

// appFocus is where the person is looking: the focused pane.
type appFocus struct {
	Pane string `json:"focused_pane"`
}

// saveFocus reads the app's focus. ok is false when it cannot be read, and
// then nothing is restored.
func saveFocus(ctx context.Context) (appFocus, bool) {
	app, err := exec.LookPath("herdr-app")
	if err != nil {
		return appFocus{}, false
	}
	out, err := appOnce(ctx, app, "status")
	var f appFocus
	if err != nil || json.Unmarshal(out, &f) != nil || f.Pane == "" {
		return appFocus{}, false
	}
	return f, true
}

// restoreFocus puts the person back where saveFocus found them (pane.focus
// also shows that pane's tab), because focusing an anchor pane in the workers
// tab shows it. Best effort: a spawn never fails over focus. Split calls both inside the project's workers lock, so one spawn
// never saves or restores another spawn's temporary focus.
func restoreFocus(ctx context.Context, f appFocus, ok bool) {
	app, err := exec.LookPath("herdr-app")
	if err != nil || !ok {
		return
	}
	_, _ = appOnce(ctx, app, "do", "pane.focus", "--pane", f.Pane)
}
