package session

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

func fastStop() StopOptions {
	return StopOptions{ExitWait: 200 * time.Millisecond, Poll: 5 * time.Millisecond}
}

// runningWorker spawns w and puts a harness (pid 4242) in its foreground,
// with the styled input line empty.
func runningWorker(t *testing.T, e *env, name string) Spawned {
	t.Helper()
	s := e.spawn(name)
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4242, Name: "claude"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.setAlive(4242, "Mon Oct 26 10:00:00 2026")
	return s
}

func stop(t *testing.T, e *env, name string) StopResult {
	t.Helper()
	res, err := Stop(context.Background(), e.deps, name, fastStop())
	if err != nil {
		t.Fatalf("stop %s: %v", name, err)
	}
	return res
}

func TestStop_LastWorkerClosesWorkersTab(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.setAlive(4242, "")
	res := stop(t, e, "w1")
	if !res.PaneClosed || !res.TabClosed || len(res.Survivors) != 0 || res.Why != "" {
		t.Fatalf("%+v", res)
	}
	if !hasCall(e.herdr.Calls(), "tab close") {
		t.Fatalf("calls %v", e.herdr.Calls())
	}
	if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndStop {
		t.Fatalf("row %+v", r)
	}
}

func TestStop_OtherWorkersKeepTab(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	runningWorker(t, e, "w1")
	runningWorker(t, e, "w2")
	e.setAlive(4242, "")
	res := stop(t, e, "w1")
	if !res.PaneClosed || res.TabClosed || hasCall(e.herdr.Calls(), "tab close") {
		t.Fatalf("%+v calls %v", res, e.herdr.Calls())
	}
	if res := stop(t, e, "w2"); !res.TabClosed {
		t.Fatalf("last worker kept the tab: %+v", res)
	}
}

func TestStop_RecordsPidsBeforeInput(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	mark := len(e.herdr.Calls())
	stop(t, e, "w1")
	calls := e.herdr.Calls()[mark:]
	pi, txt := indexOfCall(calls, "process-info "+s.Pane), indexOfCall(calls, "send-text "+s.Pane+" /exit")
	if pi < 0 || txt < 0 || pi > txt {
		t.Fatalf("process-info must come before any input: %v", calls)
	}
}

func TestStop_ClosesOnlyAtBareShell(t *testing.T) {
	e := newEnv(t) // the agent ignores /exit
	s := runningWorker(t, e, "w1")
	res := stop(t, e, "w1")
	if res.PaneClosed || hasCall(e.herdr.Calls(), "close "+s.Pane) || !strings.Contains(res.Why, "did not exit") {
		t.Fatalf("%+v calls %v", res, e.herdr.Calls())
	}
	if r := e.row(s.Run); !r.IsOpen() {
		t.Fatal("row closed although the worker still runs")
	}
	e.herdr.exitOnEnter = true
	if res := stop(t, e, "w1"); !res.PaneClosed {
		t.Fatalf("%+v", res)
	}
}

func TestStop_UnknownProcessInfoIsNotShell(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	e.herdr.pane(PaneOf(s)).known = false
	res := stop(t, e, "w1")
	if res.PaneClosed || hasCall(e.herdr.Calls(), "close ") || hasCall(e.herdr.Calls(), "send-") || !strings.Contains(res.Why, "unknown") {
		t.Fatalf("%+v calls %v", res, e.herdr.Calls())
	}
}

func TestStop_ReportsPaneClosedAndSurvivors(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	runningWorker(t, e, "w1") // pid 4242 stays alive after the pane closes
	res := stop(t, e, "w1")
	if !res.PaneClosed || len(res.Survivors) != 1 || res.Survivors[0].PID != 4242 {
		t.Fatalf("%+v", res)
	}
	// Other side: a reused pid (another start time) is not a survivor.
	e2 := newEnv(t)
	e2.herdr.exitOnEnter = true
	runningWorker(t, e2, "w1")
	calls := 0
	e2.deps.ProcStart = func(pid int) (string, bool) {
		calls++
		if calls == 1 { // recorded before input; the pid is reused after the close
			return "Mon Oct 26 10:00:00 2026", true
		}
		return "Tue Oct 27 09:00:00 2026", true
	}
	if res := stop(t, e2, "w1"); len(res.Survivors) != 0 || calls < 2 {
		t.Fatalf("%+v", res)
	}
}

func TestStop_PaneClosedFalseNamesWhy(t *testing.T) {
	e := newEnv(t)
	runningWorker(t, e, "w1")
	e.herdr.procsErr = errors.New("socket refused")
	res := stop(t, e, "w1")
	if res.PaneClosed || res.Why == "" || !strings.Contains(res.Why, "socket refused") {
		t.Fatalf("%+v", res)
	}
	if _, err := Stop(context.Background(), e.deps, "nobody", fastStop()); !errors.Is(err, ErrNoWorker) {
		t.Fatalf("err %v", err)
	}
}

func TestStop_DefaultColourDraftBlocks(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.herdr.pane(PaneOf(s)).styled = "> a person typing\n"
	res := stop(t, e, "w1")
	if res.PaneClosed || hasCall(e.herdr.Calls(), "send-") || !strings.Contains(res.Why, "draft") {
		t.Fatalf("%+v calls %v", res, e.herdr.Calls())
	}
	e.herdr.pane(PaneOf(s)).styled = "> " + dim + "a suggestion" + reset + "\n"
	if res := stop(t, e, "w1"); !res.PaneClosed {
		t.Fatalf("grey suggestion blocked stop: %+v", res)
	}
}

func TestStop_NeverSignals(t *testing.T) {
	// Behaviour: a real process recorded as the worker outlives a stop that
	// closes the pane, and one that cannot close it.
	sleeper := exec.Command("sleep", "30")
	if err := sleeper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sleeper.Process.Kill(); _ = sleeper.Wait() })
	pid := sleeper.Process.Pid
	e := newEnv(t)
	e.deps.ProcStart = nil // the real ps
	s := e.spawn("w1")
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: pid, Name: "sleep"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.herdr.exitOnEnter = true
	res := stop(t, e, "w1")
	if !res.PaneClosed || len(res.Survivors) != 1 || res.Survivors[0].PID != pid {
		t.Fatalf("%+v", res)
	}
	if _, err := os.FindProcess(pid); err != nil || !alive(pid) {
		t.Fatal("stop signalled the worker")
	}
	// Source: no file of the package sends a signal or kills.
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, _ := os.ReadFile(f)
		if _, err := parser.ParseFile(fset, f, data, parser.ImportsOnly); err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{".Signal(", ".Kill(", "syscall.Kill", "unix.Kill", "SIGTERM", "SIGKILL"} {
			if strings.Contains(string(data), bad) {
				t.Fatalf("%s contains %s", f, bad)
			}
		}
	}
}

func alive(pid int) bool {
	out, err := exec.Command("ps", "-o", "pid=", "-p", strconv.Itoa(pid)).Output()
	return err == nil && strings.TrimSpace(string(out)) == strconv.Itoa(pid)
}
