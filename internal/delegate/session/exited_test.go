package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
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
// (PR 292 review). Only its own shell-only pane is closed.
func TestWait_ExitedHarnessLeavesWorkersRecord(t *testing.T) {
	e := newEnv(t)
	a := e.worker("w1", "")
	b := e.worker("w2", "")
	e.herdr.pane(PaneOf(a)).fg = nil // only the shell is left
	mark := len(e.herdr.Calls())
	if res := e.wait("w1", 5*time.Second); res.Outcome != OutcomeGone {
		t.Fatalf("%+v", res)
	}
	for _, c := range e.herdr.Calls()[mark:] { // its own shell pane is closed; nothing is typed
		if strings.HasPrefix(c, "tab close") || strings.HasPrefix(c, "send-") || (strings.HasPrefix(c, "close ") && c != "close "+string(PaneOf(a))) {
			t.Fatalf("wait touched more than the exited worker's shell pane: %s", c)
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

// list treats an exited harness in a live pane as a worker loss, as wait
// does: the row closes gone, the worker's claims expire and its workers
// entry is dropped (PR 292 review). Only its own shell pane is closed.
func TestList_ExitedHarnessIsWorkerLoss(t *testing.T) {
	e := newEnv(t)
	a := e.worker("w1", "")
	b := e.worker("w2", "")
	e.event(b.Run, "sid-2", "session_start")
	e.claim("sid-1")
	e.claim("sid-2")
	e.herdr.pane(PaneOf(a)).fg = nil
	mark := len(e.herdr.Calls())
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.herdr.Calls()[mark:] { // its own shell pane is closed; nothing is typed
		if strings.HasPrefix(c, "tab close") || strings.HasPrefix(c, "send-") || (strings.HasPrefix(c, "close ") && c != "close "+string(PaneOf(a))) {
			t.Fatalf("list touched more than the exited worker's shell pane: %s", c)
		}
	}
	if r := e.row(a.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
		t.Fatalf("exited worker's row %+v", r)
	}
	if e.openEpisodes("sid-1") != 0 {
		t.Fatal("exited worker's claim not expired")
	}
	panes := workerPanes(t, e)
	if hasPane(panes, PaneOf(a)) {
		t.Fatalf("exited worker still placed: %v", panes)
	}
	// Other side: the live worker stays open, placed and claimed.
	if !e.row(b.Run).IsOpen() || !hasPane(panes, PaneOf(b)) || e.openEpisodes("sid-2") != 1 {
		t.Fatalf("live worker lost: row %+v panes %v", e.row(b.Run), panes)
	}
}

// A spawn of the same name, or of the same item, after the worker's harness
// exited succeeds; a live worker still blocks both.
func TestSpawn_AfterExitedWorker(t *testing.T) {
	for _, same := range []string{"name", "item"} {
		e := newEnv(t)
		a := e.worker("w1", "")
		o := e.opts("w1")
		if same == "item" {
			o = e.opts("w9")
			o.WorkItem = "feat-0a1b2c3d"
		}
		// Other side first: the live worker blocks.
		if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrLiveName) && !errors.Is(err, ErrLiveWorker) {
			t.Fatalf("same %s, live worker: %v, want a refusal", same, err)
		}
		e.herdr.pane(PaneOf(a)).fg = nil
		if _, err := Spawn(context.Background(), e.deps, o); err != nil {
			t.Fatalf("same %s after the harness exited: %v", same, err)
		}
		if r := e.row(a.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
			t.Fatalf("same %s: exited row %+v", same, r)
		}
	}
}

// stop of an exited worker closes its shell-only pane (no key is typed) and
// closes the row as gone with its claims expired, as every loss does.
func TestStop_ExitedHarnessIsWorkerLoss(t *testing.T) {
	e := newEnv(t)
	a := e.worker("w1", "")
	e.claim("sid-1")
	e.herdr.pane(PaneOf(a)).fg = nil
	mark := len(e.herdr.Calls())
	res := stop(t, e, "w1")
	for _, c := range e.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "send-") {
			t.Fatalf("stop typed into a shell-only pane: %s", c)
		}
	}
	if !res.PaneClosed {
		t.Fatalf("%+v", res)
	}
	if r := e.row(a.Run); r.EndReason != delegationledger.EndGone || e.openEpisodes("sid-1") != 0 {
		t.Fatalf("row %+v, open episodes %d", r, e.openEpisodes("sid-1"))
	}
	// Other side: a live worker's stop is a stop, and its claim is left for
	// its own SessionEnd.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	b := e2.worker("w1", "")
	e2.claim("sid-1")
	if res := stop(t, e2, "w1"); !res.PaneClosed || e2.row(b.Run).EndReason != delegationledger.EndStop || e2.openEpisodes("sid-1") != 1 {
		t.Fatalf("live stop %+v row %+v", res, e2.row(b.Run))
	}
}
