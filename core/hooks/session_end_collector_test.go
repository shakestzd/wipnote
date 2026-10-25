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

// TestSignalCollector_NoopWhenNoPIDFile verifies signalCollector is a no-op
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
	signalCollector(projectDir, "sess-test")
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
	signalCollector(dir, "sess-bad")
}

// TestSignalCollector_AlreadyExitedProcess verifies signalCollector handles
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
	signalCollector(dir, "sess-dead")
}

// TestSignalCollector_LiveProcess verifies that signalCollector successfully
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
	signalCollector(dir, sessID)
	elapsed := time.Since(start)

	// Should complete well under 3s (process exits on SIGTERM immediately).
	if elapsed > 4*time.Second {
		t.Errorf("signalCollector took too long: %v (want <4s)", elapsed)
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

// Codex kills a SessionEnd hook after 3s, so its collector wait must fit with
// headroom; other harnesses keep the full drain.
func TestCollectorDrainWait_FitsCodexCap(t *testing.T) {
	t.Setenv("WIPNOTE_HARNESS", "codex")
	if drain := collectorDrainWait(); drain >= 3*time.Second {
		t.Fatalf("codex collector drain %s, must stay under the 3s cap", drain)
	}
	t.Setenv("WIPNOTE_HARNESS", "claude")
	if drain := collectorDrainWait(); drain != 3*time.Second {
		t.Fatalf("claude collector drain = %s, want 3s", drain)
	}
}

// Codex kills SessionEnd at 3s, so the collector drain plus the telemetry
// finalize budget must leave headroom for the steps around them. Claude Code
// grants the hook its 60s timeout, so a large session gets a longer budget.
func TestTelemetryFinalizeBudget_FitsCodexCap(t *testing.T) {
	t.Setenv("WIPNOTE_HARNESS", "codex")
	if total := collectorDrainWait() + telemetryFinalizeBudget(); total > 2500*time.Millisecond {
		t.Fatalf("codex drain+finalize = %s, must leave headroom under the 3s cap", total)
	}
	t.Setenv("WIPNOTE_HARNESS", "claude")
	if b := telemetryFinalizeBudget(); b < 5*time.Second {
		t.Fatalf("claude finalize budget = %s, want at least 5s", b)
	}
}

// SessionEnd hands the session's telemetry to SessionTelemetryFn once the
// collector has exited, with a positive budget. That function ingests the
// NDJSON, materialises the rollup and only then publishes .index-offset.
func TestSessionEnd_FinalizesTelemetry(t *testing.T) {
	td := setupTestDB(t)
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
	if _, err := SessionEnd(evt, td.DB, projectDir); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}
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

// TestSignalCollector_LauncherShard verifies the collector is found under the
// launcher's OTel id (WIPNOTE_OTEL_SESSION_ID) when the hook passes the
// Claude session id.
func TestSignalCollector_LauncherShard(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM not available on Windows")
	}
	dir := t.TempDir()
	const shard, claudeID = "01alauncher", "claude-one"
	shardDir := filepath.Join(dir, ".wipnote", "sessions", shard)
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start child: %v", err)
	}
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	defer func() { _ = cmd.Process.Kill() }()
	pidPath := filepath.Join(shardDir, ".collector-pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_OTEL_SESSION_ID", shard)

	signalCollector(dir, claudeID)

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("expected launcher .collector-pid to be removed after signaling")
	}
	select {
	case <-exited:
	case <-time.After(2 * time.Second):
		t.Error("launcher collector was not signaled")
	}
}
