package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// Worker lifecycle review (feat-f1171e59): a lost worker's own shell pane is
// closed and its tab released; the terminal is pinned at spawn and checked
// before any key or close; spawn sweeps lost workers before the cap; stop
// never overwrites another close; a failed spawn leaves no live-looking row.

// Finding 1: list, wait and spawn's live check close an exited worker's own
// shell-only pane and release the tab after the last worker, as stop does.
func TestLoss_ExitedShellPaneClosedAndTabReleased(t *testing.T) {
	verbs := map[string]func(e *env){
		"list": func(e *env) { _, _ = List(context.Background(), e.deps, false) },
		"wait": func(e *env) { e.wait("w1", 5*time.Second) },
		"spawn": func(e *env) {
			o := e.opts("w9")
			o.WorkItem = "feat-0a1b2c3d"
			if _, err := Spawn(context.Background(), e.deps, o); err != nil {
				t.Fatalf("spawn: %v", err)
			}
		},
	}
	for verb, run := range verbs {
		e := newEnv(t)
		a := e.worker("w1", "")
		e.herdr.pane(PaneOf(a)).fg = nil
		tab := e.herdr.pane(PaneOf(a)).tab
		run(e)
		if !hasCall(e.herdr.Calls(), "close "+string(PaneOf(a))) || !hasCall(e.herdr.Calls(), "tab close "+string(tab)) {
			t.Fatalf("%s: the exited worker's shell pane and tab were left: %v", verb, e.herdr.Calls())
		}
	}
	// Other side: a reused pane is never closed, nor is its tab.
	e := newEnv(t)
	a := e.worker("w1", "")
	tab := e.herdr.pane(PaneOf(a)).tab
	e.herdr.pane(PaneOf(a)).terminal = "term-someone-else"
	_, _ = List(context.Background(), e.deps, false)
	if hasCall(e.herdr.Calls(), "close "+string(PaneOf(a))) || hasCall(e.herdr.Calls(), "tab close "+string(tab)) {
		t.Fatalf("a reused pane or its tab was closed: %v", e.herdr.Calls())
	}
}

// Finding 2: the terminal is pinned at spawn, so a reused pane id is seen
// before list ever adopts the agent: stop types nothing, send prompts
// nothing. An unknown terminal is refused the same way.
func TestSpawn_PinsTerminalSoReuseIsSeen(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	info, _ := ReadInfo(e.runFolder(s.Run))
	if info.TerminalID == "" || info.TerminalID != e.herdr.pane(PaneOf(s)).terminal {
		t.Fatalf("terminal not pinned at spawn: %+v", info)
	}
	e.herdr.pane(PaneOf(s)).terminal = "term-someone-else"
	mark := len(e.herdr.Calls())
	if _, err := Send(context.Background(), e.deps, "w1", "hi"); err == nil {
		t.Fatal("send to a reused pane was not refused")
	}
	res, _ := Stop(context.Background(), e.deps, "w1", fastStop())
	for _, c := range e.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "send-") || strings.HasPrefix(c, "close ") || strings.HasPrefix(c, "prompt") {
			t.Fatalf("a reused pane was touched: %s (%+v)", c, res)
		}
	}
	// Unknown terminal: nothing is typed or prompted either.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	s2 := e2.worker("w1", "")
	if _, err := UpdateInfo(e2.runFolder(s2.Run), func(i *Info) { i.TerminalID = "" }); err != nil {
		t.Fatal(err)
	}
	mark = len(e2.herdr.Calls())
	if _, err := Send(context.Background(), e2.deps, "w1", "hi"); err == nil {
		t.Fatal("send with an unknown terminal was not refused")
	}
	if res, _ := Stop(context.Background(), e2.deps, "w1", fastStop()); res.PaneClosed || !strings.Contains(res.Why, "terminal") {
		t.Fatalf("stop with an unknown terminal: %+v", res)
	}
	for _, c := range e2.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "send-") || strings.HasPrefix(c, "close ") || strings.HasPrefix(c, "prompt") {
			t.Fatalf("unknown terminal, but herdr was asked to %s", c)
		}
	}
	// Other side: the pinned terminal itself is stopped normally.
	e3 := newEnv(t)
	e3.herdr.exitOnEnter = true
	e3.worker("w1", "")
	if res := stop(t, e3, "w1"); !res.PaneClosed {
		t.Fatalf("%+v", res)
	}
}

// Finding 3: a lost worker still in the tab (exited, or its pane reused)
// does not count toward the cap, and a reused pane is never split.
func TestSpawn_LostWorkersDoNotCountTowardCap(t *testing.T) {
	for _, how := range []string{"exited", "reused"} {
		e := newEnv(t)
		var last Spawned
		for i := 1; i <= herdr.MaxWorkers; i++ {
			last = e.worker(fmt.Sprintf("w%d", i), "")
		}
		if how == "exited" {
			e.herdr.pane(PaneOf(last)).fg = nil
		} else {
			e.herdr.pane(PaneOf(last)).terminal = "term-someone-else"
		}
		if _, err := Spawn(context.Background(), e.deps, e.opts("w7")); err != nil {
			t.Fatalf("%s: spawn with a lost worker in the tab: %v", how, err)
		}
		if how == "reused" && hasCall(e.herdr.Calls(), "split "+string(PaneOf(last))) {
			t.Fatalf("split the reused pane: %v", e.herdr.Calls())
		}
		if e.row(last.Run).IsOpen() {
			t.Fatalf("%s: lost row left open", how)
		}
	}
	// Other side: six live workers are at cap (TestWait_ExitedWorkersFreeTheirSlots).
}

// Finding 4: when someone else closed the row while stop waited (a list or
// wait that saw the worker gone), stop does not overwrite the run folder's
// exit and reports the row's real end.
func TestStop_KeepsAConcurrentClose(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := e.worker("w1", "")
	rf := e.runFolder(s.Run)
	e.herdr.onEnter = func() {
		_, _ = e.deps.Ledger.CloseIfOpen(s.Run, delegationledger.CloseInfo{Ended: time.Now().UTC(), Reason: delegationledger.EndGone})
		_ = rf.Finish(1, time.Now().UTC())
		e.herdr.onEnter = nil
	}
	res := stop(t, e, "w1")
	if ri, _ := rf.Read(); ri.Exit != 1 {
		t.Fatalf("stop overwrote the other close's exit: %+v", ri)
	}
	if res.Ended != delegationledger.EndGone || e.row(s.Run).EndReason != delegationledger.EndGone {
		t.Fatalf("stop reports %q, row %+v", res.Ended, e.row(s.Run))
	}
	// Other side: a stop that closes the row itself finishes it with exit 0.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	s2 := e2.worker("w1", "")
	res = stop(t, e2, "w1")
	if ri, _ := e2.runFolder(s2.Run).Read(); !ri.Finished || ri.Exit != 0 || res.Ended != delegationledger.EndStop {
		t.Fatalf("own stop: %+v %+v", ri, res)
	}
}

// Finding 5: a spawn whose session.json update fails after the pane was
// placed closes the pane and the row, so no row is left looking live.
func TestSpawn_InfoUpdateFailureAbandons(t *testing.T) {
	e := newEnv(t)
	e.herdr.onTabCreate = func() {
		runs, _ := os.ReadDir(filepath.Join(e.deps.Store.Root(), "runs"))
		for _, r := range runs {
			p := filepath.Join(e.deps.Store.Root(), "runs", r.Name(), "session.json")
			_ = os.Remove(p)
			_ = os.Mkdir(p, 0o755) // session.json can no longer be read or replaced
		}
	}
	if _, err := Spawn(context.Background(), e.deps, e.opts("w1")); err == nil {
		t.Fatal("spawn succeeded with an unwritable session.json")
	}
	rows, _ := e.deps.Ledger.ReadAll()
	if len(rows) != 1 || rows[0].IsOpen() {
		t.Fatalf("rows %+v: want the one row closed", rows)
	}
	if !hasCall(e.herdr.Calls(), "close w1:p11") {
		t.Fatalf("the placed pane was left: %v", e.herdr.Calls())
	}
	// Other side: a spawn that writes its info keeps its row open.
	e.herdr.onTabCreate = nil
	o := e.opts("w2")
	if s, err := Spawn(context.Background(), e.deps, o); err != nil || !e.row(s.Run).IsOpen() {
		t.Fatalf("spawn: %v", err)
	}
}

// Finding 5: an open row that never got a pane, older than spawn's own
// window, no longer blocks its item; a fresh one still does.
func TestSpawn_StaleRowWithoutPaneIsLost(t *testing.T) {
	for _, age := range []time.Duration{0, 2 * paneWindow} {
		e := newEnv(t)
		row, err := e.deps.Ledger.Open(delegationledger.Record{Mode: delegationledger.ModeInteractive, Harness: HarnessClaude,
			Name: "old", WorkItem: "feat-0a1b2c3d", Started: time.Now().UTC().Add(-age)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.deps.Store.CreateRun(row.ID); err != nil {
			t.Fatal(err)
		}
		_, err = Spawn(context.Background(), e.deps, e.opts("w1"))
		if age == 0 && !errors.Is(err, ErrLiveWorker) {
			t.Fatalf("a fresh row without a pane (a spawn in progress) must block: %v", err)
		}
		if age > 0 && (err != nil || e.row(row.ID).IsOpen()) {
			t.Fatalf("a stale row without a pane still blocks: %v", err)
		}
	}
}
