package collector_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// startSleepProc starts a long-lived /bin/sh sleep process for test injection.
func startSleepProc(t *testing.T) *os.Process {
	t.Helper()
	cmd := exec.Command("/bin/sh", "-c", "sleep 60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start sleep proc: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
	return cmd.Process
}

// TestProcessCollector_Spawn_Success injects a fake SpawnFn returning
// (port=12345, a real sleep process, nil). Asserts port=12345 and cleanup is
// non-nil. Calls cleanup and asserts no panic.
func TestProcessCollector_Spawn_Success(t *testing.T) {
	var buf bytes.Buffer
	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr: &buf,
		SpawnFn: func(binPath, sessionID, projectDir string, requestedPort int) (int, *os.Process, error) {
			return 12345, startSleepProc(t), nil
		},
	})

	projectDir := t.TempDir()
	port, cleanup, err := lc.Spawn("/fake/bin", "test-sess-success", projectDir)
	if err != nil {
		t.Fatalf("Spawn returned unexpected error: %v", err)
	}
	if port != 12345 {
		t.Errorf("port = %d, want 12345", port)
	}
	if cleanup == nil {
		t.Fatal("cleanup should be non-nil on success")
	}

	// cleanup must not panic
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("cleanup panicked: %v", r)
		}
	}()
	cleanup()
}

// TestProcessCollector_Spawn_CleanupIdempotent verifies that calling the
// returned cleanup multiple times is safe — required because os.Exit in
// the launcher bypasses deferred cleanups, so the launcher calls cleanup
// explicitly before os.Exit while still leaving the deferred call in
// place for non-os.Exit paths.
func TestProcessCollector_Spawn_CleanupIdempotent(t *testing.T) {
	var buf bytes.Buffer
	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr: &buf,
		SpawnFn: func(binPath, sessionID, projectDir string, requestedPort int) (int, *os.Process, error) {
			return 7777, startSleepProc(t), nil
		},
	})

	_, cleanup, err := lc.Spawn("/fake/bin", "test-sess-idempotent", t.TempDir())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if cleanup == nil {
		t.Fatal("cleanup should be non-nil on success")
	}

	// Call cleanup twice — second call must be a no-op, not panic.
	cleanup()
	cleanup()
}

// TestProcessCollector_Spawn_SingleAttemptNoRetry verifies that Spawn makes a
// single attempt and does NOT retry on a transient failure. maxAttempts was
// reduced from 3 to 1 to bound launch latency (bug-1af3c3d9): a failing/slow
// collector must not add retry-backoff delay to every launch, and a dropped
// collector only costs a few early spans.
func TestProcessCollector_Spawn_SingleAttemptNoRetry(t *testing.T) {
	var buf bytes.Buffer
	callCount := 0

	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr: &buf,
		SpawnFn: func(binPath, sessionID, projectDir string, requestedPort int) (int, *os.Process, error) {
			callCount++
			return 0, nil, fmt.Errorf("transient error attempt %d", callCount)
		},
	})

	projectDir := t.TempDir()
	port, cleanup, err := lc.Spawn("/fake/bin", "test-sess-single", projectDir)
	if err == nil {
		t.Fatal("expected error: a single transient failure must not be retried")
	}
	if callCount != 1 {
		t.Errorf("callCount = %d, want 1 (no retry)", callCount)
	}
	if port != 0 {
		t.Errorf("port = %d, want 0 on failure", port)
	}
	if cleanup != nil {
		t.Error("cleanup should be nil on failure")
	}
}

// TestProcessCollector_Spawn_FailsAfterMaxRetries verifies that when all
// spawn attempts fail, Spawn returns a non-nil error and stderr has a FATAL line.
func TestProcessCollector_Spawn_FailsAfterMaxRetries(t *testing.T) {
	var buf bytes.Buffer
	callCount := 0

	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr: &buf,
		SpawnFn: func(binPath, sessionID, projectDir string, requestedPort int) (int, *os.Process, error) {
			callCount++
			return 0, nil, fmt.Errorf("persistent failure attempt %d", callCount)
		},
	})

	projectDir := t.TempDir()
	port, cleanup, err := lc.Spawn("/fake/bin", "test-sess-allfail", projectDir)
	if err == nil {
		t.Fatal("expected error when all spawn attempts fail, got nil")
	}
	if port != 0 {
		t.Errorf("port = %d, want 0 on failure", port)
	}
	if cleanup != nil {
		t.Error("cleanup should be nil on failure")
	}

	stderr := buf.String()
	if !strings.Contains(stderr, "FATAL") {
		t.Errorf("expected FATAL line in stderr, got: %q", stderr)
	}
}

// Once the harness runs it owns the terminal, so the per-session watchdog's
// respawn lines go to the project's collector log, never to Stderr: a line
// written there lands inside the harness's prompt (bug-40a7717c).
func TestProcessCollector_WatchdogWarningsGoToTheCollectorLog(t *testing.T) {
	t.Setenv("WIPNOTE_OTEL_WATCHDOG_INTERVAL", "50ms")
	project := t.TempDir()
	var procs []*os.Process
	var mu sync.Mutex
	spawn := func(_, _, _ string, _ int) (int, *os.Process, error) {
		cmd := exec.Command("/bin/sh", "-c", "sleep 60")
		if err := cmd.Start(); err != nil {
			return 0, nil, err
		}
		t.Cleanup(func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() })
		mu.Lock()
		procs = append(procs, cmd.Process)
		mu.Unlock()
		return 9999, cmd.Process, nil
	}
	var term bytes.Buffer
	c := collector.NewProcessCollector(collector.ProcessCollectorOpts{Stderr: &term, SpawnFn: spawn})
	_, cleanup, err := c.Spawn("/fake/bin", "sessW", project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)

	mu.Lock()
	first := procs[0]
	mu.Unlock()
	_ = first.Kill()
	_, _ = first.Wait()

	logPath := filepath.Join(project, ".wipnote", "logs", "otel-collector.log")
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, _ := os.ReadFile(logPath); strings.Contains(string(data), "collector respawned") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	data, _ := os.ReadFile(logPath)
	if !strings.Contains(string(data), "collector died") || !strings.Contains(string(data), "collector respawned") {
		t.Fatalf("collector log lacks the respawn lines:\n%s", data)
	}
	if term.Len() != 0 {
		t.Fatalf("watchdog wrote to the terminal:\n%s", term.String())
	}
}

// The real spawn path must send the collector child's own stderr to the
// collector log: the child runs for the whole session while the harness owns
// the terminal, so a line on the launcher's stderr would land in the prompt
// (bug-40a7717c). A stand-in collector prints a marker to stderr, then the
// handshake, and the marker must appear in the log.
func TestDefaultSpawnFn_ChildStderrGoesToTheCollectorLog(t *testing.T) {
	project := t.TempDir()
	bin := filepath.Join(t.TempDir(), "fake-wipnote")
	script := "#!/bin/sh\necho child-stderr-marker >&2\necho 'wipnote-otel-ready port=43210'\nexec sleep 60\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	port, proc, err := collector.DefaultSpawnFn(bin, "sessS", project, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = proc.Kill(); _, _ = proc.Wait() })
	if port != 43210 {
		t.Fatalf("port = %d, want 43210", port)
	}
	data, _ := os.ReadFile(filepath.Join(project, ".wipnote", "logs", "otel-collector.log"))
	if !strings.Contains(string(data), "child-stderr-marker") {
		t.Fatalf("collector log lacks the child's stderr:\n%s", data)
	}
}

// When the collector log cannot be opened, the launcher says so once, at
// launch, while it still owns the terminal; otherwise every later collector
// warning would vanish unseen (bug-40a7717c).
func TestProcessCollector_WarnsAtLaunchWhenCollectorLogCannotOpen(t *testing.T) {
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A file where the logs folder belongs makes the log impossible to open.
	if err := os.WriteFile(filepath.Join(project, ".wipnote", "logs"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var term bytes.Buffer
	c := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr: &term,
		SpawnFn: func(_, _, _ string, _ int) (int, *os.Process, error) {
			return 9998, startSleepProc(t), nil
		},
	})
	_, cleanup, err := c.Spawn("/fake/bin", "sessL", project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	if got := term.String(); strings.Count(got, "cannot open the collector log") != 1 {
		t.Fatalf("want one launch warning about the collector log, got:\n%s", got)
	}
	if _, err := collector.CollectorLog(project).Write([]byte("x\n")); err == nil {
		t.Fatal("CollectorLog.Write reported success for a log it could not open")
	}
}
