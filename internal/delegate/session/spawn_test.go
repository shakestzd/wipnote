package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func TestSpawn_ReturnsStartingWithin3s(t *testing.T) {
	e := newEnv(t)
	start := time.Now()
	s := e.spawn("w1")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("spawn took %s", d)
	}
	if s.Status != StatusStarting || s.Name != "w1" || s.Harness != HarnessClaude || s.Pane == "" || !runstore.ValidRunID(s.Run) {
		t.Fatalf("spawned %+v", s)
	}
	r := e.row(s.Run)
	if r.Mode != delegationledger.ModeInteractive || r.Name != "w1" || r.WorkItem != "feat-0a1b2c3d" ||
		r.CallerSession != "sess-caller" || r.Host != "host" || !r.IsOpen() {
		t.Fatalf("row %+v", r)
	}
	// Other side: spawn does not wait for the harness. A pane whose agent
	// never appears still returns starting at once.
	if hasCall(e.herdr.Calls(), "agent") {
		t.Fatalf("spawn asked herdr about the agent: %v", e.herdr.Calls())
	}
}

func TestSpawn_TypesBootLineNotBrief(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	typed := e.herdr.pane(PaneOf(s)).typed
	if len(typed) != 1 {
		t.Fatalf("typed %q", typed)
	}
	line := typed[0]
	if !strings.Contains(line, "'delegate' 'boot'") || !strings.Contains(line, s.Run) ||
		!strings.Contains(line, RunEnv+"="+Quote(s.Run)) || !strings.Contains(line, CallerEnv+"=") {
		t.Fatalf("boot line %q", line)
	}
	if strings.Contains(line, "Do the thing") || strings.Contains(line, "\n") || len(line) > 512 {
		t.Fatalf("brief or a long line typed: %q", line)
	}
	brief, _, err := e.runFolder(s.Run).ReadFrom(runstore.FileBrief, 0)
	if err != nil || !strings.Contains(string(brief), "Do the thing.\nIn detail.") || !strings.Contains(string(brief), "-result.json") {
		t.Fatalf("brief.md %q err %v", brief, err)
	}
	info, err := ReadInfo(e.runFolder(s.Run))
	if err != nil || info.Pane != s.Pane || info.Task == "" || info.Name != "w1" {
		t.Fatalf("info %+v err %v", info, err)
	}
}

func TestSpawn_WaitsForShellBeforeTyping(t *testing.T) {
	e := newEnv(t)
	e.herdr.shellAfter = 3
	s := e.spawn("w1")
	calls := e.herdr.Calls()
	run := indexOfCall(calls, "run "+s.Pane)
	n := 0
	for _, c := range calls[:max(run, 0)] {
		if c == "process-info "+s.Pane {
			n++
		}
	}
	if run < 0 || n < 4 {
		t.Fatalf("typed after %d process-info calls: %v", n, calls)
	}
	// Other side: a shell that never settles is never typed into.
	e2 := newEnv(t)
	e2.herdr.shellAfter = 1 << 20
	o := e2.opts("w2")
	o.ShellWait = 50 * time.Millisecond
	_, err := Spawn(context.Background(), e2.deps, o)
	if !errors.Is(err, ErrShellNotReady) || hasCall(e2.herdr.Calls(), "run ") {
		t.Fatalf("err %v calls %v", err, e2.herdr.Calls())
	}
	rows, _ := e2.deps.Ledger.ReadAll()
	if len(rows) != 1 || rows[0].IsOpen() || rows[0].EndReason != delegationledger.EndGone {
		t.Fatalf("a failed spawn must close its row: %+v", rows)
	}
}

func TestSpawn_FirstWorkerCreatesWorkersTab(t *testing.T) {
	e := newEnv(t)
	e.spawn("w1")
	calls := e.herdr.Calls()
	if !hasCall(calls, "tab create w1 workers") || hasCall(calls, "split ") {
		t.Fatalf("calls %v", calls)
	}
}

func TestSpawn_SecondWorkerSplitsWorkersTab(t *testing.T) {
	e := newEnv(t)
	a := e.spawn("w1")
	b := e.spawn("w2")
	calls := e.herdr.Calls()
	if n := strings.Count(strings.Join(calls, "\n"), "tab create"); n != 1 {
		t.Fatalf("%d tabs created: %v", n, calls)
	}
	if !hasCall(calls, "split "+a.Pane+" right -> "+b.Pane) {
		t.Fatalf("second worker not split right of the first: %v", calls)
	}
	// The caller's own pane is never split.
	if hasCall(calls, "split w1:p1 ") || e.herdr.pane(PaneOf(b)).tab != e.herdr.pane(PaneOf(a)).tab {
		t.Fatalf("calls %v", calls)
	}
}

func TestSpawn_RefusesSeventhWorker(t *testing.T) {
	e := newEnv(t)
	for i := 1; i <= 6; i++ {
		e.spawn("w" + string(rune('0'+i)))
	}
	before := len(e.herdr.Calls())
	runsBefore, _ := os.ReadDir(filepath.Join(e.deps.Store.Root(), "runs"))
	_, err := Spawn(context.Background(), e.deps, e.opts("w7"))
	if !errors.Is(err, ErrAtCap) || !strings.Contains(err.Error(), "at cap") {
		t.Fatalf("err %v", err)
	}
	rows, _ := e.deps.Ledger.ReadAll()
	runsAfter, _ := os.ReadDir(filepath.Join(e.deps.Store.Root(), "runs"))
	for _, c := range e.herdr.Calls()[before:] {
		if !readOnly(c) { // spawn observes its workers before the cap
			t.Fatalf("at cap but herdr was asked to %q", c)
		}
	}
	if len(rows) != 6 || len(runsAfter) != len(runsBefore) {
		t.Fatalf("at cap created something: %d rows, runs %d -> %d", len(rows), len(runsBefore), len(runsAfter))
	}
}

func TestSpawn_RefusesLiveName(t *testing.T) {
	e := newEnv(t)
	a := e.spawn("w1")
	if _, err := Spawn(context.Background(), e.deps, e.opts("w1")); !errors.Is(err, ErrLiveName) {
		t.Fatalf("err %v", err)
	}
	// Other side: once the pane is gone the name is free, and the gone row is
	// closed first.
	_ = e.herdr.Close(context.Background(), PaneOf(a))
	b := e.spawn("w1")
	if r := e.row(a.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone || b.Run == a.Run {
		t.Fatalf("old row %+v", r)
	}
}

func TestSpawn_RefusesWithoutWorkItem(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	o.WorkItem = ""
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrNoWorkItem) {
		t.Fatalf("err %v", err)
	}
	// An empty NAME is derived from the work item (slice 15), so with no
	// item the refusal is still "no work item", never a bad name.
	o = e.opts("")
	o.WorkItem = ""
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrNoWorkItem) {
		t.Fatalf("no name, no item: err %v", err)
	}
	o = e.opts("")
	o.WorkItem = "0bad-item"
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadName) {
		t.Fatalf("derived name not starting with a letter: err %v", err)
	}
	for _, bad := range []string{"W1", "-w", strings.Repeat("a", 33), "w_1"} {
		o := e.opts(bad)
		if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadName) {
			t.Fatalf("name %q: err %v", bad, err)
		}
	}
	o = e.opts("w1")
	o.Harness = "gemini"
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadHarness) {
		t.Fatalf("err %v", err)
	}
	if rows, _ := e.deps.Ledger.ReadAll(); len(rows) != 0 || len(e.herdr.Calls()) != 0 {
		t.Fatalf("refusal wrote %v / called %v", rows, e.herdr.Calls())
	}
	if s := e.spawn("w1"); s.Status != StatusStarting {
		t.Fatalf("%+v", s)
	}
}

func TestSpawn_RefusesOutsideHerdr(t *testing.T) {
	e := newEnv(t)
	e.deps.Herdr = nil
	if _, err := Spawn(context.Background(), e.deps, e.opts("w1")); !errors.Is(err, ErrNotInHerdr) {
		t.Fatalf("err %v", err)
	}
}
