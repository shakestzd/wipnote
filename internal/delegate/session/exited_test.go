package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
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

// An exited harness in a live pane is a worker loss for wait too: the run
// leaves the caller's workers record, so it no longer takes a worker slot
// (PR 292 review). The pane itself is left alone.
func TestWait_ExitedHarnessLeavesWorkersRecord(t *testing.T) {
	e := newEnv(t)
	a := e.worker("w1", "")
	b := e.worker("w2", "")
	e.herdr.pane(PaneOf(a)).fg = nil // only the shell is left
	mark := len(e.herdr.Calls())
	if res := e.wait("w1", 5*time.Second); res.Outcome != OutcomeGone {
		t.Fatalf("%+v", res)
	}
	for _, c := range e.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "close ") || strings.HasPrefix(c, "tab close") || strings.HasPrefix(c, "send-") {
			t.Fatalf("wait touched the pane: %s", c)
		}
	}
	panes := workerPanes(t, e)
	if hasPane(panes, PaneOf(a)) {
		t.Fatalf("exited worker %s still in the workers record %v", PaneOf(a), panes)
	}
	// Other side: the live worker keeps its slot.
	if !hasPane(panes, PaneOf(b)) {
		t.Fatalf("live worker %s dropped: %v", PaneOf(b), panes)
	}
}

// Seven workers whose harness exited, each seen by wait, then a spawn: the
// dead ones hold no slot, so the spawn is not refused at cap.
func TestWait_ExitedWorkersFreeTheirSlots(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 7; i++ {
		name := fmt.Sprintf("w%d", i)
		s := e.worker(name, "")
		e.herdr.pane(PaneOf(s)).fg = nil
		if res := e.wait(name, 5*time.Second); res.Outcome != OutcomeGone {
			t.Fatalf("%s: %+v", name, res)
		}
	}
	if _, err := Spawn(context.Background(), e.deps, e.opts("w8")); err != nil {
		t.Fatalf("spawn after seven exited workers: %v", err)
	}
	// Other side: six live workers do fill the tab.
	e2 := newEnv(t)
	for i := 1; i <= herdr.MaxWorkers; i++ {
		e2.spawn(fmt.Sprintf("w%d", i))
	}
	if _, err := Spawn(context.Background(), e2.deps, e2.opts("w7")); !errors.Is(err, ErrAtCap) {
		t.Fatalf("seventh live worker: %v, want at cap", err)
	}
}
