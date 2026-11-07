package bgproc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// Fake children: the test binary re-executes itself with a wipnote-shaped
// argv. TestMain runs the child before the testing flags are parsed, so the
// argv only has to look like the real process to the doctor's argv check.
const childEnv = "WIPNOTE_BGPROC_TEST_CHILD"

func TestMain(m *testing.M) {
	switch os.Getenv(childEnv) {
	case "":
		os.Exit(m.Run())
	case "sleep":
		time.Sleep(time.Hour)
	case "stubborn": // ignores SIGTERM, so only SIGKILL stops it
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	case "health":
		serveChildHealth()
	}
	os.Exit(0)
}

// serveChildHealth answers /healthz with this pid on a free port and writes
// the port to the file named by WIPNOTE_BGPROC_TEST_PORTFILE.
func serveChildHealth() {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.Exit(2)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = os.WriteFile(os.Getenv("WIPNOTE_BGPROC_TEST_PORTFILE"), []byte(strconv.Itoa(port)), 0o644)
	_ = http.Serve(ln, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(collector.Health{PID: os.Getpid(), Mode: "shared"})
	}))
}

// startChild starts a fake child in mode with argv args and reaps it when it
// exits, so a stopped child does not linger as a zombie.
func startChild(t *testing.T, mode string, extraEnv []string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(append(os.Environ(), childEnv+"="+mode), extraEnv...)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill() // the test's own child, never a real process
		<-done
	})
	return cmd
}

func mkProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func writeLease(t *testing.T, root string, pid int, heartbeat time.Time) {
	t.Helper()
	rec := daemon.LeaseRecord{Format: 2, Token: "tok", PID: pid, StartedAt: heartbeat, HeartbeatAt: heartbeat}
	data, _ := json.Marshal(rec)
	if err := os.WriteFile(daemon.LeasePath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testOpts() Options {
	return Options{Grace: 300 * time.Millisecond, ProbeTimeout: 300 * time.Millisecond}
}

func find(t *testing.T, procs []Proc, pid int) Proc {
	t.Helper()
	for _, p := range procs {
		if p.PID == pid {
			return p
		}
	}
	t.Fatalf("pid %d not reported; got %+v", pid, procs)
	return Proc{}
}

// A writer that holds the lease but whose read socket never answers is hung.
func TestInspect_WriterWithDeadSocketIsHung(t *testing.T) {
	root := mkProject(t)
	child := startChild(t, "sleep", nil, "--project-dir", root, "_serve-child", "--headless")
	writeLease(t, root, child.Process.Pid, time.Now())

	procs, err := Inspect(context.Background(), root, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if p := find(t, procs, child.Process.Pid); p.Kind != KindWriter || p.State != StateHung {
		t.Fatalf("writer = %+v, want hung writer", p)
	}
}

func TestInspect_SharedCollectorAnsweringHealthIsHealthy(t *testing.T) {
	root := mkProject(t)
	portFile := filepath.Join(t.TempDir(), "port")
	child := startChild(t, "health", []string{"WIPNOTE_BGPROC_TEST_PORTFILE=" + portFile},
		"otel-collect", "--shared", "--project-dir", root, "--listen", "127.0.0.1:0")
	port := waitPort(t, portFile)
	writeShared(t, root, child.Process.Pid, port)

	procs, err := Inspect(context.Background(), root, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if p := find(t, procs, child.Process.Pid); p.Kind != KindSharedCollector || p.State != StateHealthy {
		t.Fatalf("collector = %+v, want healthy shared collector", p)
	}
}

// A PID file naming a process whose argv is not wipnote's is never trusted.
func TestInspect_ArgvMismatchIsUnverified(t *testing.T) {
	root := mkProject(t)
	other := mkProject(t)
	child := startChild(t, "sleep", nil, "--project-dir", other, "_serve-child", "--headless")
	writeLease(t, root, child.Process.Pid, time.Now())

	procs, err := Inspect(context.Background(), root, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if p := find(t, procs, child.Process.Pid); p.State != StateUnverified {
		t.Fatalf("foreign-project writer = %+v, want unverified", p)
	}
}

// Without --restart-all nothing is signalled; with it, a hung process is
// stopped with SIGTERM, and one that ignores SIGTERM with SIGKILL.
func TestRestart_StopsHungAndOrphanedOnly(t *testing.T) {
	root := mkProject(t)
	hung := startChild(t, "stubborn", nil, "--project-dir", root, "_serve-child", "--headless")
	writeLease(t, root, hung.Process.Pid, time.Now())
	orphanSess := filepath.Join(root, ".wipnote", "sessions", "s1")
	_ = os.MkdirAll(orphanSess, 0o755)
	foreign := startChild(t, "sleep", nil, "unrelated", "--project-dir", root)
	collector.WriteCollectorPID(root, "s1", foreign.Process.Pid)

	procs, err := Inspect(context.Background(), root, testOpts())
	if err != nil {
		t.Fatal(err)
	}
	if !collector.PIDAlive(hung.Process.Pid) {
		t.Fatal("Inspect signalled a process")
	}
	var restarted []string
	opts := testOpts()
	opts.RestartWriter = func(context.Context, string) error { restarted = append(restarted, "writer"); return nil }
	results := Restart(context.Background(), root, procs, opts)

	r := findResult(t, results, hung.Process.Pid)
	if r.Err != nil || !r.Killed {
		t.Fatalf("stubborn hung writer result = %+v, want SIGKILL escalation", r)
	}
	waitGone(t, hung.Process.Pid)
	if !collector.PIDAlive(foreign.Process.Pid) {
		t.Fatal("restart touched a process whose argv is not wipnote's")
	}
	if len(restarted) != 1 {
		t.Fatalf("writer restarts = %v", restarted)
	}
}

func findResult(t *testing.T, results []StopResult, pid int) StopResult {
	t.Helper()
	for _, r := range results {
		if r.Proc.PID == pid {
			return r
		}
	}
	t.Fatalf("no stop result for pid %d: %+v", pid, results)
	return StopResult{}
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for collector.PIDAlive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("pid %d still alive", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitPort(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil {
			if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return n
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("fake collector never reported its port")
	return 0
}

func writeShared(t *testing.T, root string, pid, port int) {
	t.Helper()
	data := fmt.Sprintf(`{"pid":%d,"port":%d,"started_at":%q}`, pid, port, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(collector.SharedLeasePath(root), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}
