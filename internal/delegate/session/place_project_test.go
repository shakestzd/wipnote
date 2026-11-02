package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Two delegators in one project share its workers tab and its cap, and one
// stopping its worker leaves the other's in place.
func TestSpawn_TwoCallersShareTheProjectTab(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	a := runningWorker(t, e, "w1")
	other := e.opts("w2")
	other.CallerSession, other.CallerPane = "sess-other", herdr.PaneID("w1:p9")
	b, err := Spawn(t.Context(), e.deps, other)
	if err != nil {
		t.Fatal(err)
	}
	panes := workerPanes(t, e)
	if len(panes) != 2 || !hasPane(panes, herdr.PaneID(a.Pane)) || !hasPane(panes, herdr.PaneID(b.Pane)) {
		t.Fatalf("panes %v, want %s and %s in one record", panes, a.Pane, b.Pane)
	}
	if res := stop(t, e, "w1"); res.TabClosed {
		t.Fatal("the tab closed with a worker left in it")
	}
	if panes := workerPanes(t, e); len(panes) != 1 || !hasPane(panes, herdr.PaneID(b.Pane)) {
		t.Fatalf("after stop: panes %v, want only %s", panes, b.Pane)
	}
}

// Different projects get different tab keys, and one project always the same.
func TestWorkersKey_PerProject(t *testing.T) {
	one, err := runstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	two, err := runstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if workersKey(one) == workersKey(two) {
		t.Fatal("two projects share a workers tab key")
	}
	if workersKey(one) != workersKey(one) {
		t.Fatal("a project's key changed")
	}
}

// Workers still running from a version with per-caller records keep their slots:
// the old files are merged into the project's record once, and then removed.
func TestWithWorkers_MergesLegacyRecords(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Dir(workersPath(e.deps.Store))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name string, w Workers) string {
		data, err := json.Marshal(w)
		if err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	one := write("t1.json", Workers{Tab: &herdr.WorkersTab{ID: "tab-1"}, Panes: []WorkerPane{{Pane: "p1", Run: "r1"}, {Pane: "p2", Run: "r2"}}})
	two := write("t2.json", Workers{Panes: []WorkerPane{{Pane: "p3", Run: "r3"}}})
	if err := withWorkers(e.deps.Store, func(w *Workers) error {
		if len(w.Panes) != 3 || w.Tab == nil || w.Tab.ID != "tab-1" {
			t.Fatalf("merged record %+v", w)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{one, two} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s was not removed: %v", p, err)
		}
	}
	if got := workerPanes(t, e); len(got) != 3 {
		t.Fatalf("panes after merge = %v", got)
	}
}

// The merged workers count toward the cap.
func TestWithWorkers_LegacyWorkersCountTowardCap(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Dir(workersPath(e.deps.Store))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var w Workers
	for i := 0; i < herdr.MaxWorkers; i++ {
		w.Panes = append(w.Panes, WorkerPane{Pane: herdr.PaneID("p" + string(rune('a'+i))), Run: "r"})
	}
	data, _ := json.Marshal(w)
	if err := os.WriteFile(filepath.Join(dir, "t1.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := withWorkers(e.deps.Store, func(got *Workers) error {
		if checkCap(got) == nil {
			t.Fatalf("a full legacy record passed the cap: %d panes", len(got.Panes))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
