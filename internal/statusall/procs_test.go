package statusall

import (
	"bytes"
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/procsweep"
)

// fakeLister is a fake process table; it counts its calls.
type fakeLister struct {
	procs []procsweep.RawProc
	err   error
	calls atomic.Int32
}

func (f *fakeLister) List(context.Context) ([]procsweep.RawProc, error) {
	f.calls.Add(1)
	return f.procs, f.err
}

func rawProc(pid int, cmd, session string, started time.Time) procsweep.RawProc {
	return procsweep.RawProc{PID: pid, PPID: 1, Command: cmd, StartedAt: started,
		Env: map[string]string{"CLAUDE_CODE_SESSION_ID": session}}
}

// procsFixture: project a has an ended session (sessA) and project b a live
// one (sessB); the table holds one process of each.
func procsFixture(t *testing.T) (Options, *fakeLister, string) {
	t.Helper()
	a := tempProject(t, "c1", testNow.Add(-time.Hour))
	b := tempProject(t, "c2", testNow.Add(-2*time.Hour))
	openSession(t, a, sessA, "claude", testNow.Add(-3*time.Hour), testNow.Add(-2*time.Hour))
	openSession(t, b, sessB, "claude", testNow.Add(-time.Hour), time.Time{})
	f := &fakeLister{procs: []procsweep.RawProc{
		rawProc(4242, "node "+a+"/server.js --port 3000", sessA, testNow.Add(-150*time.Minute)),
		rawProc(5151, "vite --watch", sessB, testNow.Add(-30*time.Minute)),
	}}
	o := baseOptions(entry(a, "a", testNow), entry(b, "b", testNow))
	o.Procs = f
	return o, f, a
}

func TestCollect_OrphanShowsOnlyUnderItsProjectAndNeedsYou(t *testing.T) {
	o, f, a := procsFixture(t)
	r := Collect(context.Background(), o)

	if f.calls.Load() != 1 {
		t.Fatalf("sweep listed processes %d times, want once per run", f.calls.Load())
	}
	pa := findProject(t, r, "a")
	if len(pa.Orphans) != 1 || pa.Orphans[0].PID != 4242 || pa.Orphans[0].StopHint != "kill 4242" ||
		pa.Orphans[0].Project != "a" || pa.Orphans[0].SessionID != sessA || !pa.Orphans[0].Orphan {
		t.Fatalf("a orphans = %+v", pa.Orphans)
	}
	if strings.Contains(pa.Orphans[0].Command, a) || !strings.Contains(pa.Orphans[0].Command, "./server.js") {
		t.Fatalf("orphan command must not carry the host path: %q", pa.Orphans[0].Command)
	}
	if pa.Sources["procs"] != SourceOK {
		t.Fatalf("procs source = %q", pa.Sources["procs"])
	}
	var got []string
	for _, n := range pa.NeedsYou {
		if n.Kind == NeedOrphanProcess {
			got = append(got, n.What)
		}
	}
	want := "orphaned process 4242 — node ./server.js --port 3000; stop with: kill 4242"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("orphan needs = %q, want [%q]", got, want)
	}
	pb := findProject(t, r, "b")
	if len(pb.Orphans) != 0 || len(pb.NeedsYou) != 0 {
		t.Fatalf("a live process must show nowhere: orphans %+v needs %+v", pb.Orphans, pb.NeedsYou)
	}

	var buf bytes.Buffer
	WriteText(&buf, r, testNow)
	if !strings.Contains(buf.String(), "orphan    4242 node ./server.js --port 3000 · stop: kill 4242") ||
		!strings.Contains(buf.String(), "NEEDS YOU orphan_process: "+want) {
		t.Fatalf("text lacks the orphan lines:\n%s", buf.String())
	}
}

func TestCollect_SweepErrorIsNotAvailableAndTheRestPrints(t *testing.T) {
	o, f, _ := procsFixture(t)
	f.err = procsweep.ErrEnvUnreadable
	r := Collect(context.Background(), o)
	pa := findProject(t, r, "a")
	if pa.Sources["procs"] != SourceNotAvailable+": "+procsweep.ErrEnvUnreadable.Error() {
		t.Fatalf("procs source = %q", pa.Sources["procs"])
	}
	if len(pa.Orphans) != 0 || len(pa.Errors) != 0 || pa.LastCommit == nil {
		t.Fatalf("row = %+v; the rest must still read", pa)
	}
}

func TestCollect_NoProcsSkipsTheSweep(t *testing.T) {
	o, f, _ := procsFixture(t)
	o.Procs = nil
	o.ProcsReason = SourceSkipped
	r := Collect(context.Background(), o)
	if f.calls.Load() != 0 {
		t.Fatalf("sweep ran %d times with procs skipped", f.calls.Load())
	}
	if pa := findProject(t, r, "a"); pa.Sources["procs"] != SourceSkipped || len(pa.Orphans) != 0 {
		t.Fatalf("procs source = %q orphans %+v", pa.Sources["procs"], pa.Orphans)
	}
}

// A hung lister is cut by the sweep's own bound, not the whole run's.
func TestCollect_HungSweepIsBounded(t *testing.T) {
	o, _, _ := procsFixture(t)
	o.Procs = procsweep.ListerFunc(func(ctx context.Context) ([]procsweep.RawProc, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	o.ProcsTimeout = 200 * time.Millisecond
	start := time.Now()
	r := Collect(context.Background(), o)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Collect took %s with a hung sweep", d)
	}
	if s := findProject(t, r, "a").Sources["procs"]; !strings.HasPrefix(s, SourceNotAvailable) {
		t.Fatalf("procs source = %q", s)
	}
}

// A sweep stuck in a filesystem read that ignores the context (a stalled
// project disk) must not hold the report past the sweep's bound.
func TestCollect_StalledSweepReadIsBounded(t *testing.T) {
	o, _, _ := procsFixture(t)
	o.ProcsTimeout = 200 * time.Millisecond
	release := make(chan struct{})
	defer close(release)
	o.sweep = func(context.Context, []procsweep.Project, procsweep.Options) (procsweep.Result, error) {
		<-release // blocks and never looks at ctx
		return procsweep.Result{}, nil
	}
	start := time.Now()
	r := Collect(context.Background(), o)
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("Collect took %s with a stalled sweep read", d)
	}
	pa := findProject(t, r, "a")
	if s := pa.Sources["procs"]; !strings.HasPrefix(s, SourceNotAvailable) {
		t.Fatalf("procs source = %q", s)
	}
	if pa.LastCommit == nil {
		t.Fatalf("row = %+v; the rest must still read", pa)
	}
}
