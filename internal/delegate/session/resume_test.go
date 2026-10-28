package session

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// stoppedWorker spawns name, binds the given harness sessions (one per
// /clear) through hook-style events and stops it. It returns the old run.
func stoppedWorker(t *testing.T, e *env, o SpawnOptions, sids ...string) string {
	t.Helper()
	e.herdr.exitOnEnter = true
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatalf("spawn %s: %v", o.Name, err)
	}
	for _, sid := range sids {
		e.event(s.Run, sid, "session_start")
	}
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4242, Name: o.Harness})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	if res := stop(t, e, o.Name); !res.PaneClosed {
		t.Fatalf("stop %s: %+v", o.Name, res)
	}
	return s.Run
}

// resumeOpts is what the command passes: a new name, the caller's own cwd
// (resume must ignore it) and no harness or work item.
func (e *env) resumeOpts(name string) SpawnOptions {
	o := e.opts(name)
	o.Harness, o.WorkItem, o.Cwd = "", "", otherDir(e)
	return o
}

func otherDir(e *env) string {
	d := filepath.Join(e.dir, "elsewhere")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func resume(t *testing.T, e *env, o SpawnOptions, from string) (Spawned, Info) {
	t.Helper()
	s, err := Resume(context.Background(), e.deps, o, from)
	if err != nil {
		t.Fatalf("resume %s from %s: %v", o.Name, from, err)
	}
	info, err := ReadInfo(e.runFolder(s.Run))
	if err != nil {
		t.Fatal(err)
	}
	return s, info
}

// written is what a refused resume must leave unchanged.
type written struct {
	rows, runs, calls int
}

func snapshot(t *testing.T, e *env) written {
	t.Helper()
	rows, _ := e.deps.Ledger.ReadAll()
	runs, _ := os.ReadDir(filepath.Join(e.deps.Store.Root(), "runs"))
	return written{len(rows), len(runs), len(e.herdr.Calls())}
}

func refuseResume(t *testing.T, e *env, o SpawnOptions, from, want string) {
	t.Helper()
	before := snapshot(t, e)
	_, err := Resume(context.Background(), e.deps, o, from)
	if !errors.Is(err, ErrCannotResume) || !strings.Contains(err.Error(), want) {
		t.Fatalf("resume %s from %s: %v, want ErrCannotResume naming %q", o.Name, from, err, want)
	}
	if after := snapshot(t, e); after != before {
		t.Fatalf("a refused resume wrote: before %+v after %+v", before, after)
	}
}

func TestResume_ClaudeUsesLastSessionID(t *testing.T) {
	e := newEnv(t)
	old := stoppedWorker(t, e, e.opts("w1"), "sid-a", "sid-b")
	s, info := resume(t, e, e.resumeOpts("w2"), "w1")
	if info.ResumeSession != "sid-b" || info.Harness != HarnessClaude || s.Harness != HarnessClaude {
		t.Fatalf("info %+v: want the last session sid-b, harness claude", info)
	}
	argv := BootArgv("/bin/wipnote", info, "brief")
	want := []string{"/bin/wipnote", "claude", "--in-place", "--resume", "sid-b", "--", "brief"}
	if !slices.Equal(argv, want) {
		t.Fatalf("argv %q, want %q (no --name on a resume)", argv, want)
	}
	// Other side: a fresh spawn's argv has --name and no --resume.
	fresh, _ := ReadInfo(e.runFolder(old))
	if argv := BootArgv("/bin/wipnote", fresh, ""); slices.Contains(argv, "--resume") || !slices.Contains(argv, "--name") {
		t.Fatalf("fresh argv %q", argv)
	}
	// The ledger's own worker_sessions count too (a list moved them there).
	e2 := newEnv(t)
	old2 := stoppedWorker(t, e2, e2.opts("w1"))
	if _, err := e2.deps.Ledger.Enrich(old2, delegationledger.Enrichment{WorkerSessions: []string{"sid-l"}}); err != nil {
		t.Fatal(err)
	}
	if _, info := resume(t, e2, e2.resumeOpts("w2"), "w1"); info.ResumeSession != "sid-l" {
		t.Fatalf("resume_session %q, want the ledger's sid-l", info.ResumeSession)
	}
}

func TestResume_CodexUsesResumeID(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	o.Harness = HarnessCodex
	stoppedWorker(t, e, o, "thr-1")
	r := e.resumeOpts("w2")
	r.Brief = ""
	_, info := resume(t, e, r, "w1")
	argv := BootArgv("/bin/wipnote", info, "")
	if want := []string{"/bin/wipnote", "codex", "--in-place", "--resume", "thr-1"}; !slices.Equal(argv, want) {
		t.Fatalf("argv %q, want %q", argv, want)
	}
	// Other side: a brief cannot be given to codex resume (unverified), so
	// it is refused with that reason and nothing is written.
	e2 := newEnv(t)
	o2 := e2.opts("w1")
	o2.Harness = HarnessCodex
	stoppedWorker(t, e2, o2, "thr-1")
	refuseResume(t, e2, e2.resumeOpts("w2"), "w1", "delegate send")
	// --harness must match the recorded run.
	bad := e2.resumeOpts("w2")
	bad.Brief, bad.Harness = "", HarnessClaude
	refuseResume(t, e2, bad, "w1", "harness")
}

func TestResume_SameCwdAndPlace(t *testing.T) {
	e := newEnv(t)
	o := e.opts("w1")
	o.Cwd = filepath.Join(e.dir, "sub")
	if err := os.MkdirAll(o.Cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	stoppedWorker(t, e, o, "sid-a")
	s, info := resume(t, e, e.resumeOpts("w2"), "w1")
	if info.Cwd != o.Cwd || info.Place != PlaceHost || e.row(s.Run).Host != PlaceHost {
		t.Fatalf("info %+v: want cwd %s, place host", info, o.Cwd)
	}
	// Other side: a recorded cwd that is gone refuses.
	e.herdr.exitOnEnter = true
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4243, Name: "claude"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.event(s.Run, "sid-a", "session_start")
	stop(t, e, "w2")
	if err := os.RemoveAll(o.Cwd); err != nil {
		t.Fatal(err)
	}
	refuseResume(t, e, e.resumeOpts("w3"), "w2", "folder")
}

func TestResume_SameContainerPlace(t *testing.T) {
	e, _, _ := containerEnv(t)
	stoppedWorker(t, e, e.containerOpts("w1", HarnessClaude), "sid-c")
	// --container on a host run, or none on a container run: the recorded
	// place wins only when the flag is not given.
	s, info := resume(t, e, e.resumeOpts("w2"), "w1")
	if info.Place != PlaceContainer || info.Root != e.dir || e.row(s.Run).Host != PlaceContainer {
		t.Fatalf("info %+v: want place container, root %s", info, e.dir)
	}
	argv, err := ContainerBoot(context.Background(), e.deps.Store, s.Run, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	i := slices.Index(argv, "wipnote")
	if i < 0 || !slices.Equal(argv[i:i+5], []string{"wipnote", "claude", "--in-place", "--resume", "sid-c"}) {
		t.Fatalf("container argv %q: want wipnote claude --in-place --resume sid-c", argv)
	}
	// Other side: a host run cannot be resumed with --container.
	h := newEnv(t)
	stoppedWorker(t, h, h.opts("w1"), "sid-h")
	r := h.resumeOpts("w2")
	r.Container, r.Root = true, h.dir
	refuseResume(t, h, r, "w1", "host")
}

func TestResume_NewRowHasResumedFrom(t *testing.T) {
	e := newEnv(t)
	old := stoppedWorker(t, e, e.opts("w1"), "sid-a")
	s, info := resume(t, e, e.resumeOpts("w2"), "w1")
	r := e.row(s.Run)
	if s.Run == old || r.ResumedFrom != old || info.ResumedFrom != old || !r.IsOpen() || r.Name != "w2" {
		t.Fatalf("new row %+v: want its own open row resumed_from %s", r, old)
	}
	if r.WorkItem != "feat-0a1b2c3d" {
		t.Fatalf("work item %q: want the old run's", r.WorkItem)
	}
	if o := e.row(old); o.IsOpen() || o.EndReason != delegationledger.EndStop {
		t.Fatalf("old row %+v: must stay closed as stop", o)
	}
	// Other side: a fresh spawn has no resumed_from, and the old run cannot
	// be resumed a second time (resume the newest run of the chain).
	f := e.spawn("w3")
	if e.row(f.Run).ResumedFrom != "" {
		t.Fatalf("fresh row %+v", e.row(f.Run))
	}
	refuseResume(t, e, e.resumeOpts("w4"), "w1", s.Run)
}

func TestResume_RefusesLiveName(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	e.event(s.Run, "sid-a", "session_start")
	refuseResume(t, e, e.resumeOpts("w2"), "w1", "live")
	// Other side: once stopped, the same resume works.
	e.herdr.exitOnEnter = true
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4242, Name: "claude"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	stop(t, e, "w1")
	resume(t, e, e.resumeOpts("w2"), "w1")
	// No run by that name at all.
	refuseResume(t, e, e.resumeOpts("w5"), "nobody", "no worker")
}

func TestResume_RefusesNoSessionID(t *testing.T) {
	e := newEnv(t)
	old := stoppedWorker(t, e, e.opts("w1"))
	refuseResume(t, e, e.resumeOpts("w2"), "w1", "session id")
	// Other side: once a bind event is on record, the resume works.
	e.event(old, "sid-late", "session_start")
	if _, info := resume(t, e, e.resumeOpts("w2"), "w1"); info.ResumeSession != "sid-late" {
		t.Fatalf("resume_session %q", info.ResumeSession)
	}
}

func TestResume_RefusesSweptRunFolder(t *testing.T) {
	e := newEnv(t)
	old := stoppedWorker(t, e, e.opts("w1"), "sid-a")
	dir := e.runFolder(old).Dir()
	if err := os.Rename(dir, dir+".kept"); err != nil {
		t.Fatal(err)
	}
	refuseResume(t, e, e.resumeOpts("w2"), "w1", "swept")
	// Other side: with the folder back, the resume works.
	if err := os.Rename(dir+".kept", dir); err != nil {
		t.Fatal(err)
	}
	resume(t, e, e.resumeOpts("w2"), "w1")
}

// TestResume_RetryAfterAbandonedAttempt: a resume whose shell never settled
// was abandoned before the harness started (end_reason gone, no bound
// session), so the original run can be resumed again. Other side: a resume
// that launched and bound a session still blocks a second resume.
func TestResume_RetryAfterAbandonedAttempt(t *testing.T) {
	e := newEnv(t)
	old := stoppedWorker(t, e, e.opts("w1"), "sid-a")
	e.herdr.shellAfter = 1 << 20
	o := e.resumeOpts("w2")
	o.ShellWait = 30 * time.Millisecond
	if _, err := Resume(context.Background(), e.deps, o, "w1"); !errors.Is(err, ErrShellNotReady) {
		t.Fatalf("first attempt: %v, want ErrShellNotReady", err)
	}
	e.herdr.shellAfter = 0
	s, info := resume(t, e, e.resumeOpts("w3"), "w1")
	if info.ResumeSession != "sid-a" || e.row(s.Run).ResumedFrom != old {
		t.Fatalf("retry info %+v row %+v", info, e.row(s.Run))
	}
	// Other side: w3 launched and bound sid-a; once stopped it still counts.
	e.event(s.Run, "sid-a", "session_start")
	e.herdr.exitOnEnter = true
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4243, Name: "claude"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	stop(t, e, "w3")
	refuseResume(t, e, e.resumeOpts("w4"), "w1", s.Run)
}

// TestResume_NewerRunWithoutBindUsesRecordedSession: a resumed run stopped
// before its SessionStart was seen still names the session it resumed
// (resume_session), so following the "resume that one instead" advice works.
// Other side: a fresh run with no bind and no resume_session still refuses.
func TestResume_NewerRunWithoutBindUsesRecordedSession(t *testing.T) {
	e := newEnv(t)
	stoppedWorker(t, e, e.opts("w1"), "sid-a")
	s, _ := resume(t, e, e.resumeOpts("w2"), "w1")
	e.herdr.exitOnEnter = true
	e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4243, Name: "claude"})
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	stop(t, e, "w2")
	if _, info := resume(t, e, e.resumeOpts("w3"), "w2"); info.ResumeSession != "sid-a" {
		t.Fatalf("resume_session %q, want w2's recorded sid-a", info.ResumeSession)
	}
	e2 := newEnv(t)
	stoppedWorker(t, e2, e2.opts("w1"))
	refuseResume(t, e2, e2.resumeOpts("w2"), "w1", "session id")
}
