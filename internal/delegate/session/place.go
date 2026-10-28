package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// workersDir holds one file per caller pane: its workers tab and its live
// worker panes in spawn order (decision L: one workers tab per caller).
const workersDir = "workers"

// WorkerPane is one worker pane of a caller's workers tab.
type WorkerPane struct {
	Pane herdr.PaneID `json:"pane"`
	Run  string       `json:"run"`
}

// Workers is a caller's workers tab record.
type Workers struct {
	Tab   *herdr.WorkersTab `json:"tab,omitempty"`
	Panes []WorkerPane      `json:"panes"`
}

func (w Workers) paneIDs() []herdr.PaneID {
	ids := make([]herdr.PaneID, 0, len(w.Panes))
	for _, p := range w.Panes {
		ids = append(ids, p.Pane)
	}
	return ids
}

// dropRun removes run's pane from the record.
func (w *Workers) dropRun(run string) {
	var keep []WorkerPane
	for _, p := range w.Panes {
		if p.Run != run {
			keep = append(keep, p)
		}
	}
	w.Panes = keep
}

var unsafeKey = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

func workersPath(store *runstore.Store, callerPane string) string {
	key := unsafeKey.ReplaceAllString(callerPane, "_")
	if key == "" {
		key = "cur"
	}
	return filepath.Join(store.Root(), workersDir, key+".json")
}

// withWorkers runs fn on the caller's record under its lock and saves the
// record when fn returns nil.
func withWorkers(store *runstore.Store, callerPane string, fn func(*Workers) error) error {
	path := workersPath(store, callerPane)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("delegate: workers record: %w", err)
	}
	release := filelock.Guard(path)
	defer release()
	var w Workers
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &w); err != nil {
			return fmt.Errorf("delegate: unreadable workers record %s: %w", path, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := fn(&w); err != nil {
		return err
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	return filelock.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// prune drops the panes herdr says are gone. Any other lookup failure is
// returned, so a flaky query never frees capacity.
func prune(ctx context.Context, c herdr.Client, w *Workers) error {
	var keep []WorkerPane
	for _, p := range w.Panes {
		_, err := c.Get(ctx, p.Pane)
		if herdr.IsAbsent(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("delegate: herdr cannot say whether pane %s lives: %w", p.Pane, err)
		}
		keep = append(keep, p)
	}
	w.Panes = keep
	return nil
}

// checkCap refuses before anything is created when the tab is full (Phase
// B run 6: "at cap" is its own outcome).
func checkCap(w *Workers) error {
	if n := len(w.Panes); n >= herdr.MaxWorkers {
		return fmt.Errorf("%w: %d live workers in this caller's workers tab (cap %d); stop one first", ErrAtCap, n, herdr.MaxWorkers)
	}
	return nil
}

// place gives the next worker a pane in the caller's workers tab and records
// it. herdr.Place creates the tab for the first worker and splits it after.
func place(ctx context.Context, c herdr.Client, w *Workers, run, cwd string) (herdr.PaneID, error) {
	tab, pane, err := herdr.Place(ctx, c, w.Tab, w.paneIDs(), "workers", cwd)
	if err != nil {
		return "", err
	}
	if tab != w.Tab {
		w.Panes = nil // a new tab: the old record's panes are all gone
	}
	w.Tab = tab
	w.Panes = append(w.Panes, WorkerPane{Pane: pane, Run: run})
	return pane, nil
}

// release drops pane from the record and closes the workers tab once no
// worker is left. It reports whether the tab was closed.
func release(ctx context.Context, c herdr.Client, w *Workers, pane herdr.PaneID) (bool, error) {
	var keep []WorkerPane
	for _, p := range w.Panes {
		if p.Pane != pane {
			keep = append(keep, p)
		}
	}
	w.Panes = keep
	if err := prune(ctx, c, w); err != nil {
		return false, err
	}
	closed, err := herdr.ReleaseTab(ctx, c, w.Tab, w.paneIDs())
	if closed {
		w.Tab = nil
	}
	return closed, err
}
