package session

import (
	"context"
	"strings"
	"testing"
)

// A worker whose harness exited but whose pane survives (a crash, or a
// person quit it) reads as gone, not as its last hook state (PR 292 review).
func TestList_ExitedHarnessInLivePaneIsGone(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1")
	bindHook(t, e, s.Run, "sid-one")
	e.event(s.Run, "sid-one", "prompt")
	e.herdr.pane(PaneOf(s)).fg = nil // only the shell is left
	rows, err := List(context.Background(), e.deps, false)
	if err != nil || len(rows) != 1 {
		t.Fatalf("%+v %v", rows, err)
	}
	if rows[0].State != StateGone || !strings.Contains(rows[0].Detail, "exited") {
		t.Fatalf("exited worker reads %s: %q", rows[0].State, rows[0].Detail)
	}
}

// Other side: a live harness keeps its hook state, and an unknown
// foreground is never read as an exit.
func TestList_LiveOrUnknownForegroundIsNotGone(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1")
	bindHook(t, e, s.Run, "sid-one")
	rows, _ := List(context.Background(), e.deps, false)
	if len(rows) != 1 || rows[0].State != StateReady {
		t.Fatalf("live worker: %+v", rows)
	}
	e.herdr.pane(PaneOf(s)).fg = nil
	e.herdr.pane(PaneOf(s)).known = false
	rows, _ = List(context.Background(), e.deps, false)
	if len(rows) != 1 || rows[0].State == StateGone {
		t.Fatalf("unknown foreground read as gone: %+v", rows)
	}
}
