package session

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Slice 22: a Claude one-shot run is an interactive worker that wipnote
// ends itself. These cover the session pieces it reuses: launch args after
// the brief, and a stop that may interrupt, signal and record an exit.

func TestBootArgv_LaunchArgsAfterBrief(t *testing.T) {
	info := Info{Name: "w1", Harness: HarnessClaude, Args: []string{"--mcp-config", "m.json", "--session-id", "abc"}}
	got := BootArgv("/bin/wipnote", info, "the brief")
	want := []string{"/bin/wipnote", "claude", "--in-place", "--name", "w1", "--", "the brief", "--mcp-config", "m.json", "--session-id", "abc"}
	if !slices.Equal(got, want) {
		t.Fatalf("argv %q, want %q", got, want)
	}
	// No launch args: the argv is as before.
	info.Args = nil
	if got := BootArgv("/bin/wipnote", info, "the brief"); !slices.Equal(got, want[:7]) {
		t.Fatalf("argv %q", got)
	}
}

func TestSpawn_LaunchArgsRecorded(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	var seen string
	o.SessionID, o.Transcript = "sid-fixed", "/t/sid-fixed.jsonl"
	o.LaunchArgs = func(rf *runstore.RunFolder) ([]string, error) {
		seen = rf.ID()
		return []string{"--settings", rf.Dir() + "/claude-settings.json"}, nil
	}
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	info, err := ReadInfo(e.runFolder(s.Run))
	if err != nil {
		t.Fatal(err)
	}
	if seen != s.Run || len(info.Args) != 2 || info.SessionID != "sid-fixed" || info.Transcript != "/t/sid-fixed.jsonl" {
		t.Fatalf("seen %q info %+v", seen, info)
	}
	// Without LaunchArgs nothing is recorded.
	s2 := e.spawn("w2")
	if info, _ := ReadInfo(e.runFolder(s2.Run)); info.Args != nil || info.SessionID != "" {
		t.Fatalf("info %+v", info)
	}
}

func TestStop_ExitRecorded(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.setAlive(4242, "")
	exit := 1
	o := fastStop()
	o.Exit = &exit
	if _, err := Stop(context.Background(), e.deps, "w1", o); err != nil {
		t.Fatal(err)
	}
	r := e.row(s.Run)
	if r.EndReason != delegationledger.EndExit || r.Exit == nil || *r.Exit != 1 {
		t.Fatalf("row %+v", r)
	}
	if got, _ := os.ReadFile(filepath.Join(e.runFolder(s.Run).Dir(), runstore.FileExit)); string(got) != "1\n" {
		t.Fatalf("run folder exit %q", got)
	}
	// Without Exit the row ends as stop.
	s2 := runningWorker(t, e, "w2")
	e.setAlive(4242, "")
	stop(t, e, "w2")
	if r := e.row(s2.Run); r.EndReason != delegationledger.EndStop || r.Exit != nil {
		t.Fatalf("row %+v", r)
	}
}

func TestWorkerProcs_OwnPaneOnly(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1")
	procs, err := WorkerProcs(context.Background(), e.deps, s.Run)
	if err != nil || len(procs) != 1 || procs[0].PID != 4242 || procs[0].Start == "" {
		t.Fatalf("procs %+v err %v", procs, err)
	}
	if !SameProc(e.deps, procs[0]) {
		t.Fatal("the recorded process still runs")
	}
	e.setAlive(4242, "Tue Oct 27 09:00:00 2026") // the pid was given to another process
	if SameProc(e.deps, procs[0]) {
		t.Fatal("a reused pid is not the worker's")
	}
	// A pane that now holds another terminal names no processes.
	e.herdr.mu.Lock()
	e.herdr.panes[PaneOf(s)].terminal = "term-other"
	e.herdr.mu.Unlock()
	if procs, err := WorkerProcs(context.Background(), e.deps, s.Run); err == nil || procs != nil {
		t.Fatalf("reused pane: procs %+v err %v", procs, err)
	}
}

func TestStop_InterruptPressesEscapeFirst(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.event(s.Run, "sid-1", runstore.KindSessionStart)
	o := fastStop()
	o.Interrupt = true
	if _, err := Stop(context.Background(), e.deps, "w1", o); err != nil {
		t.Fatal(err)
	}
	calls := e.herdr.Calls()
	esc, exit := indexOfCall(calls, "send-keys "+string(PaneOf(s))+" escape"), indexOfCall(calls, "send-text "+string(PaneOf(s))+" /exit")
	if esc < 0 || exit < 0 || esc > exit {
		t.Fatalf("escape before /exit: %v", calls)
	}
	// Without Interrupt no escape is pressed.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	s2 := runningWorker(t, e2, "w1")
	e2.event(s2.Run, "sid-1", runstore.KindSessionStart)
	stop(t, e2, "w1")
	if hasCall(e2.herdr.Calls(), "send-keys "+string(PaneOf(s2))+" escape") {
		t.Fatalf("escape pressed without Interrupt: %v", e2.herdr.Calls())
	}
}

func TestStop_ExitTypedOnlyWhenTyped(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1") // /exit is typed and ignored
	res := stop(t, e, "w1")
	if res.PaneClosed || !res.ExitTyped {
		t.Fatalf("%+v", res)
	}
	// A draft on the line: nothing is typed, so ExitTyped stays false.
	e.herdr.pane(PaneOf(s)).styled = "> half a sentence\n"
	if res := stop(t, e, "w1"); res.ExitTyped || res.PaneClosed {
		t.Fatalf("%+v", res)
	}
}

func TestBootArgv_WorkDir(t *testing.T) {
	info := Info{Name: "w1", Harness: HarnessClaude, WorkDir: "/r/wt"}
	got := BootArgv("/bin/wipnote", info, "b")
	if !slices.Equal(got, []string{"/bin/wipnote", "claude", "--in-place", "--work-dir", "/r/wt", "--name", "w1", "--", "b"}) {
		t.Fatalf("argv %q", got)
	}
	info.WorkDir = ""
	if slices.Contains(BootArgv("/bin/wipnote", info, "b"), "--work-dir") {
		t.Fatal("no work dir, no flag")
	}
}

func TestStop_NoTypeNeverTypes(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	o := fastStop()
	o.NoType = true
	res, err := Stop(context.Background(), e.deps, "w1", o)
	if err != nil {
		t.Fatal(err)
	}
	if hasCall(e.herdr.Calls(), "send-") || res.PaneClosed || res.ExitTyped || res.Why == "" {
		t.Fatalf("res %+v calls %v", res, e.herdr.Calls())
	}
	// At a bare shell a NoType stop closes the pane.
	e.herdr.mu.Lock()
	e.herdr.panes[PaneOf(s)].fg = nil
	e.herdr.mu.Unlock()
	if res, _ := Stop(context.Background(), e.deps, "w1", o); !res.PaneClosed {
		t.Fatalf("res %+v", res)
	}
}
