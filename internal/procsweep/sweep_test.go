package procsweep

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

const (
	liveSession  = "11111111-1111-4111-8111-111111111111"
	endedSession = "22222222-2222-4222-8222-222222222222"
	otherSession = "33333333-3333-4333-8333-333333333333"
)

// tempProject writes a sessions ledger with one open and one ended session
// into a fresh temp .wipnote dir and returns it as a Project.
func tempProject(t *testing.T, name string) Project {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	dir := filepath.Join(t.TempDir(), ".wipnote")
	store := sessionledger.NewStore(dir)
	start := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{liveSession, endedSession} {
		if _, err := store.Open(sessionledger.Record{SessionID: id, Harness: "claude", StartedAt: start}); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
	}
	if err := store.Close(endedSession, start.Add(time.Hour)); err != nil {
		t.Fatalf("close: %v", err)
	}
	return Project{Name: name, WipnoteDir: dir}
}

func fakeTable(procs ...RawProc) Lister {
	return ListerFunc(func(context.Context) ([]RawProc, error) { return procs, nil })
}

func raw(pid int, cmd string, env map[string]string) RawProc {
	return RawProc{PID: pid, PPID: 1, Command: cmd, StartedAt: time.Date(2026, 10, 27, 9, 5, 0, 0, time.UTC), Env: env}
}

func sweepOne(t *testing.T, l Lister) Result {
	t.Helper()
	res, err := Sweep(context.Background(), []Project{tempProject(t, "demo")}, Options{Lister: l, Self: -1})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return res
}

func byPID(res Result, pid int) (Proc, bool) {
	for _, p := range res.Procs {
		if p.PID == pid {
			return p, true
		}
	}
	return Proc{}, false
}

func TestSweep_ChildWithSessionIDIsFound(t *testing.T) {
	res := sweepOne(t, fakeTable(raw(101, "npm run dev", map[string]string{"CLAUDE_CODE_SESSION_ID": liveSession})))
	p, ok := byPID(res, 101)
	if !ok {
		t.Fatalf("process with a session id was not found: %+v", res)
	}
	if p.SessionID != liveSession || p.Project != "demo" || p.Orphan {
		t.Fatalf("live child misreported: %+v", p)
	}
	if p.StopHint != "kill 101" {
		t.Fatalf("stop hint = %q, want %q", p.StopHint, "kill 101")
	}
}

func TestSweep_ProcessWithoutSessionIDIsIgnored(t *testing.T) {
	res := sweepOne(t, fakeTable(raw(102, "vim notes.txt", map[string]string{"HOME": "/nonexistent"})))
	if len(res.Procs) != 0 {
		t.Fatalf("process without a session id must be ignored, got %+v", res.Procs)
	}
}

func TestSweep_ChildOfEndedSessionIsOrphan(t *testing.T) {
	res := sweepOne(t, fakeTable(
		raw(103, "nohup python -m http.server", map[string]string{"CLAUDE_CODE_SESSION_ID": endedSession}),
		raw(104, "tail -f log", map[string]string{"CLAUDE_CODE_SESSION_ID": liveSession}),
	))
	orphans := res.Orphans()
	if len(orphans) != 1 || orphans[0].PID != 103 {
		t.Fatalf("want only pid 103 orphaned, got %+v", orphans)
	}
	if p, _ := byPID(res, 104); p.Orphan {
		t.Fatalf("live session's child must not be an orphan: %+v", p)
	}
}

func TestSweep_UnknownSessionIsNotAttributed(t *testing.T) {
	res := sweepOne(t, fakeTable(raw(105, "sleep 99", map[string]string{"CLAUDE_CODE_SESSION_ID": otherSession})))
	if len(res.Procs) != 0 || res.Unattributed != 1 {
		t.Fatalf("session unknown to every ledger: want 0 procs and 1 unattributed, got %+v", res)
	}
}

func TestSweep_ExcludesSelfAndAncestors(t *testing.T) {
	env := map[string]string{"CLAUDE_CODE_SESSION_ID": liveSession}
	shell := raw(200, "bash -c wipnote reconcile", env)
	self := raw(201, "wipnote reconcile", env)
	self.PPID = 200
	res, err := Sweep(context.Background(), []Project{tempProject(t, "demo")},
		Options{Lister: fakeTable(shell, self, raw(202, "watcher", env)), Self: 201})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Procs) != 1 || res.Procs[0].PID != 202 {
		t.Fatalf("sweep must skip itself and its ancestors, got %+v", res.Procs)
	}
}

func TestSweep_CommandIsTrimmed(t *testing.T) {
	long := "node " + strings.Repeat("x", 300)
	res := sweepOne(t, fakeTable(raw(106, long, map[string]string{"CLAUDE_CODE_SESSION_ID": liveSession})))
	p, _ := byPID(res, 106)
	if len([]rune(p.Command)) > maxCommandRunes {
		t.Fatalf("command not trimmed: %d runes", len([]rune(p.Command)))
	}
}

func TestSweep_PlatformErrorIsSurfaced(t *testing.T) {
	l := ListerFunc(func(context.Context) ([]RawProc, error) { return nil, ErrEnvUnreadable })
	_, err := Sweep(context.Background(), []Project{tempProject(t, "demo")}, Options{Lister: l, Self: -1})
	if !errors.Is(err, ErrEnvUnreadable) {
		t.Fatalf("want ErrEnvUnreadable, got %v", err)
	}
	if !strings.Contains(ErrEnvUnreadable.Error(), "process environment not readable on this platform") {
		t.Fatalf("error text must say so plainly: %q", ErrEnvUnreadable.Error())
	}
}

func TestSweep_TimeBound(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	l := ListerFunc(func(context.Context) ([]RawProc, error) { <-block; return nil, nil })
	start := time.Now()
	_, err := Sweep(context.Background(), []Project{tempProject(t, "demo")},
		Options{Lister: l, Self: -1, Timeout: 50 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("want ErrTimeout from a stuck lister, got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("sweep did not honour its time bound")
	}
}

// endedAt is when tempProject's endedSession closed.
var endedAt = time.Date(2026, 10, 27, 10, 0, 0, 0, time.UTC)

func endedChildStartedAt(t *testing.T, start time.Time) Proc {
	t.Helper()
	rp := raw(300, "npm run dev", map[string]string{"CLAUDE_CODE_SESSION_ID": endedSession})
	rp.StartedAt = start
	p, ok := byPID(sweepOne(t, fakeTable(rp)), 300)
	if !ok {
		t.Fatal("ended session's child not found")
	}
	return p
}

func TestSweep_StartedBeforeEndIsOrphan(t *testing.T) {
	if p := endedChildStartedAt(t, endedAt.Add(-time.Minute)); !p.Orphan {
		t.Fatalf("a process from before the session ended is a leftover: %+v", p)
	}
}

// claude --resume reuses the id but the ledger row stays closed, so a
// process started after the recorded end belongs to the resumed run.
func TestSweep_StartedAfterEndIsResumedNotOrphan(t *testing.T) {
	if p := endedChildStartedAt(t, endedAt.Add(time.Minute)); p.Orphan {
		t.Fatalf("a process started after the end belongs to a resumed session: %+v", p)
	}
}

func TestSweep_StartedWithinToleranceIsOrphan(t *testing.T) {
	if p := endedChildStartedAt(t, endedAt.Add(endClockTolerance-time.Millisecond)); !p.Orphan {
		t.Fatalf("a start within the clock tolerance of the end is still a leftover: %+v", p)
	}
}

func TestSweep_UnknownStartKeepsEndedRule(t *testing.T) {
	if p := endedChildStartedAt(t, time.Time{}); !p.Orphan {
		t.Fatalf("with no start time, an ended session's child stays an orphan: %+v", p)
	}
}

// unreadableProject is a project whose sessions ledger path is a directory,
// so reading it fails.
func unreadableProject(t *testing.T, name string) Project {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(filepath.Join(dir, sessionledger.FileName), 0o755); err != nil {
		t.Fatal(err)
	}
	return Project{Name: name, WipnoteDir: dir}
}

func TestSweep_BadLedgerFailsOnlyItsProject(t *testing.T) {
	projects := []Project{unreadableProject(t, "broken"), tempProject(t, "healthy")}
	l := fakeTable(raw(400, "nohup worker", map[string]string{"CLAUDE_CODE_SESSION_ID": endedSession}))
	res, err := Sweep(context.Background(), projects, Options{Lister: l, Self: -1})
	if err != nil {
		t.Fatalf("one bad ledger must not fail the sweep: %v", err)
	}
	orphans := res.Orphans()
	if len(orphans) != 1 || orphans[0].PID != 400 || orphans[0].Project != "healthy" {
		t.Fatalf("healthy project's orphan must still be found, got %+v", res)
	}
	msg, ok := res.ProjectErrors["broken"]
	if !ok || msg == "" || len(res.ProjectErrors) != 1 {
		t.Fatalf("want one error naming project %q, got %v", "broken", res.ProjectErrors)
	}
	if strings.Contains(msg, projects[0].WipnoteDir) {
		t.Fatalf("project error must not carry a host path: %q", msg)
	}
}

func TestSweep_HealthyLedgersRecordNoProjectErrors(t *testing.T) {
	if res := sweepOne(t, fakeTable()); len(res.ProjectErrors) != 0 {
		t.Fatalf("no ledger failed, want no project errors, got %v", res.ProjectErrors)
	}
}
