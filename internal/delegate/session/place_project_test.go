package session

import (
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
