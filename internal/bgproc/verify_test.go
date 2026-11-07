package bgproc

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/internal/procsweep"
	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// fakeTable returns a Lister whose rows come from rows(), called per List.
func fakeTable(rows func() []procsweep.RawProc) procsweep.Lister {
	return procsweep.ListerFunc(func(context.Context) ([]procsweep.RawProc, error) { return rows(), nil })
}

// A per-session collector reparented to init lost its launcher.
func TestInspect_SessionCollectorWithDeadLauncherIsOrphaned(t *testing.T) {
	root := mkProject(t)
	_ = os.MkdirAll(filepath.Join(root, ".wipnote", "sessions", "s1"), 0o755)
	collector.WriteCollectorPID(root, "s1", 4000001)
	cmd := "wipnote otel-collect --session-id s1 --project-dir " + root + " --listen 127.0.0.1:0 --no-idle-timeout"
	opts := testOpts()
	opts.Lister = fakeTable(func() []procsweep.RawProc {
		return []procsweep.RawProc{{PID: 4000001, PPID: 1, Command: cmd}}
	})
	procs, err := Inspect(context.Background(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if p := find(t, procs, 4000001); p.Kind != KindSessionCollector || p.State != StateOrphaned {
		t.Fatalf("collector = %+v, want orphaned session collector", p)
	}
}

// The pid is re-verified right before the signal: if it now runs something
// else (pid reuse), nothing is sent.
func TestRestart_PIDReusedAfterInspectIsNotSignalled(t *testing.T) {
	root := mkProject(t)
	child := startChild(t, "sleep", nil, "--project-dir", root, "_serve-child", "--headless")
	pid := child.Process.Pid
	p := Proc{Kind: KindWriter, PID: pid, State: StateHung,
		Command: "wipnote --project-dir " + root + " _serve-child --headless"}
	opts := testOpts()
	opts.Lister = fakeTable(func() []procsweep.RawProc {
		return []procsweep.RawProc{{PID: pid, PPID: 1, Command: "vim notes.txt"}}
	})
	results := Restart(context.Background(), root, []Proc{p}, opts)
	if len(results) != 1 || !errors.Is(results[0].Err, ErrNoLongerOurs) {
		t.Fatalf("results = %+v, want ErrNoLongerOurs", results)
	}
	if !collector.PIDAlive(pid) {
		t.Fatal("a re-used pid was signalled")
	}
}
