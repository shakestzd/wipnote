package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// TestSignalCollector_NoopWhenNoPIDFile verifies signalCollectorDirWait is a no-op
// when the .collector-pid file does not exist.
func TestSignalCollector_NoopWhenNoPIDFile(t *testing.T) {
	dir := t.TempDir()
	projectDir := dir
	// Create .wipnote/sessions/<sid>/ but no .collector-pid file.
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions", "sess-test")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Must not panic or error.
	signalCollectorDirWait(projectDir, "sess-test", 3*time.Second, true)
}

// TestSignalCollector_InvalidPIDFile verifies graceful handling of a corrupt PID file.
func TestSignalCollector_InvalidPIDFile(t *testing.T) {
	dir := t.TempDir()
	sessDir := filepath.Join(dir, ".wipnote", "sessions", "sess-bad")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessDir, ".collector-pid"), []byte("notanumber\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Must not panic.
	signalCollectorDirWait(dir, "sess-bad", 3*time.Second, true)
}

// TestSignalCollector_AlreadyExitedProcess verifies signalCollectorDirWait handles
// a PID that refers to an already-exited process without panicking.
func TestSignalCollector_AlreadyExitedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not available on Windows")
	}

	dir := t.TempDir()
	sessDir := filepath.Join(dir, ".wipnote", "sessions", "sess-dead")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Write a PID that is guaranteed not to exist (use a large, unlikely PID).
	// On Linux, PID max is typically 4194304.
	if err := os.WriteFile(filepath.Join(sessDir, ".collector-pid"), []byte("9999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Must not panic even if FindProcess succeeds but Signal fails.
	signalCollectorDirWait(dir, "sess-dead", 3*time.Second, true)
}

// TestSignalCollector_LiveProcess verifies that signalCollectorDirWait successfully
// SIGTERMs a running child process and removes the PID file afterward.
func TestSignalCollector_LiveProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not available on Windows")
	}

	dir := t.TempDir()
	sessID := "sess-live"
	sessDir := filepath.Join(dir, ".wipnote", "sessions", sessID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Start a long-running child process (sleep 60).
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	defer func() { _ = cmd.Process.Kill(); _, _ = cmd.Process.Wait() }()

	// Write its PID.
	pidPath := filepath.Join(sessDir, ".collector-pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n", cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}

	// Signal the "collector".
	start := time.Now()
	signalCollectorDirWait(dir, sessID, 3*time.Second, true)
	elapsed := time.Since(start)

	// Should complete well under 3s (process exits on SIGTERM immediately).
	if elapsed > 4*time.Second {
		t.Errorf("signalCollectorDirWait took too long: %v (want <4s)", elapsed)
	}

	// PID file must be removed.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected .collector-pid to be removed after signaling")
	}

	// Child process must no longer be running.
	if err := cmd.Process.Signal(os.Signal(nil)); err == nil {
		// Try to confirm process is gone.
		pidStr := strconv.Itoa(cmd.Process.Pid)
		out, _ := exec.Command("ps", "-p", pidStr).Output()
		if len(out) > 0 {
			t.Log("process may still be running:", string(out))
		}
	}
}

// SessionEnd hands the session's telemetry to SessionTelemetryFn once the
// collector has exited, with a positive budget. That function ingests the
// NDJSON, materialises the rollup and only then publishes .index-offset.
func TestSessionEnd_FinalizesTelemetry(t *testing.T) {
	setupTestDB(t)
	projectDir := t.TempDir()
	sessionID := "sess-finalize-telemetry"
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_HARNESS", "claude")

	var calls int
	var gotProject, gotSession string
	var gotBudget time.Duration
	orig := SessionTelemetryFn
	SessionTelemetryFn = func(p, s string, budget time.Duration) error {
		calls++
		gotProject, gotSession, gotBudget = p, s, budget
		return nil
	}
	t.Cleanup(func() { SessionTelemetryFn = orig })

	evt := &CloudEvent{SessionID: sessionID, CWD: projectDir}
	if _, err := SessionEnd(evt, projectDir); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
	CatchUpEndedSessions(projectDir, "", true) // deferred handoff, features, telemetry (bug-1daed6d6)
	if calls != 1 {
		t.Fatalf("SessionTelemetryFn called %d times, want 1", calls)
	}
	if gotProject != projectDir || gotSession != sessionID {
		t.Fatalf("SessionTelemetryFn(%q, %q), want (%q, %q)", gotProject, gotSession, projectDir, sessionID)
	}
	if gotBudget <= 0 {
		t.Fatalf("budget = %s, want > 0", gotBudget)
	}
}

// The writer records "<pid>\n<starttime>". The two-line file must parse to the
// pid, not be rejected as invalid.
func TestSignalCollectorDir_TwoLinePIDFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not available on Windows")
	}
	prev := CollectorAliveFn
	t.Cleanup(func() { CollectorAliveFn = prev })
	CollectorAliveFn = nil

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	dir := t.TempDir()
	sessDir := filepath.Join(dir, ".wipnote", "sessions", "sess-two")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf("%d\n3574637\n", cmd.Process.Pid)
	if err := os.WriteFile(filepath.Join(sessDir, ".collector-pid"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	signalCollectorDirWait(dir, "sess-two", 3*time.Second, true)
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("two-line PID file was not parsed: collector never signalled")
	}
}

// With the liveness seam wired, a dead or reused pid is never signalled.
func TestSignalCollectorDir_UsesAliveGuard(t *testing.T) {
	prev := CollectorAliveFn
	t.Cleanup(func() { CollectorAliveFn = prev })
	CollectorAliveFn = func(string) (bool, int) { return false, 4242 }

	dir := t.TempDir()
	sessDir := filepath.Join(dir, ".wipnote", "sessions", "sess-reuse")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	pidPath := filepath.Join(sessDir, ".collector-pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d\n1\n", os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	signalCollectorDirWait(dir, "sess-reuse", 3*time.Second, true) // would SIGTERM the test binary if unguarded
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("stale pid file not removed")
	}
}
