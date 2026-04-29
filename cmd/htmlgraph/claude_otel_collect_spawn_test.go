package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// TestGenerateOtelSessionID verifies OTel session ID generation produces
// unique, non-empty strings with the expected format.
func TestGenerateOtelSessionID(t *testing.T) {
	id1 := generateOtelSessionID()
	if id1 == "" {
		t.Fatal("generateOtelSessionID returned empty string")
	}
	id2 := generateOtelSessionID()
	if id2 == "" {
		t.Fatal("generateOtelSessionID returned empty string")
	}
	if id1 == id2 {
		t.Errorf("two calls returned same ID: %q", id1)
	}
	// 12 hex timestamp + 16 hex entropy = 28 chars
	if len(id1) != 28 {
		t.Errorf("session ID length = %d, want 28: %q", len(id1), id1)
	}
}

// TestSpawnCollector_HandshakeAndPort spawns a real otel-collect child,
// asserts the handshake returns a valid port, and verifies the process
// is alive.
func TestSpawnCollector_HandshakeAndPort(t *testing.T) {
	bin := buildOtelCollectTestBinary(t)
	projectDir := mkOtelCollectProject(t)

	port, proc, err := spawnCollector(bin, "test-spawn-hs", projectDir)
	if err != nil {
		t.Fatalf("spawnCollector: %v", err)
	}
	t.Cleanup(func() {
		_ = proc.Kill()
		_, _ = proc.Wait()
	})

	if port <= 0 || port > 65535 {
		t.Errorf("port out of range: %d", port)
	}

	// Process should be alive — kill -0 check (signal 0 probes existence).
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("collector process not alive: %v", err)
	}
}

// TestSpawnCollector_BindFailure tests that a non-existent binary path
// causes spawnCollector to return an error without leaking a process.
func TestSpawnCollector_BindFailure(t *testing.T) {
	projectDir := mkOtelCollectProject(t)

	port, proc, err := spawnCollector("/nonexistent/binary", "test-bindfail", projectDir)
	if err == nil {
		if proc != nil {
			_ = proc.Kill()
			_, _ = proc.Wait()
		}
		t.Fatal("expected error for non-existent binary, got nil")
	}
	if port != 0 {
		t.Errorf("expected port 0 on error, got %d", port)
	}
	if proc != nil {
		t.Error("expected nil process on error")
	}
}

// TestSpawnCollector_HandshakeTimeout verifies that spawnCollector returns
// an error when the child does not print a handshake line within the timeout.
// We simulate this by spawning a binary that never prints the expected line.
func TestSpawnCollector_HandshakeTimeout(t *testing.T) {
	// Use "sleep" as the binary — it will never print a handshake.
	// spawnCollector should timeout and kill it.
	port, proc, err := spawnCollector("sleep", "test-timeout", t.TempDir())
	if err == nil {
		if proc != nil {
			_ = proc.Kill()
			_, _ = proc.Wait()
		}
		t.Fatal("expected error for non-handshaking binary, got nil")
	}
	if port != 0 {
		t.Errorf("expected port 0 on error, got %d", port)
	}
	if proc != nil {
		t.Error("expected nil process on error")
	}
	if !strings.Contains(err.Error(), "handshake") && !strings.Contains(err.Error(), "timeout") &&
		!strings.Contains(err.Error(), "start") {
		t.Errorf("error should mention handshake/timeout/start, got: %v", err)
	}
}

// TestWriteCollectorPID writes a PID file and reads it back.
func TestWriteCollectorPID(t *testing.T) {
	projectDir := t.TempDir()
	sid := "test-pid-write"
	pid := 42

	writeCollectorPID(projectDir, sid, pid)

	pidPath := filepath.Join(projectDir, ".htmlgraph", "sessions", sid, ".collector-pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("PID file not found at %s: %v", pidPath, err)
	}

	got, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("PID file content is not a valid integer: %q", string(data))
	}
	if got != pid {
		t.Errorf("PID = %d, want %d", got, pid)
	}
}

// TestWriteCollectorPID_CreatesDirectories verifies that writeCollectorPID
// creates the necessary directory structure.
func TestWriteCollectorPID_CreatesDirectories(t *testing.T) {
	projectDir := t.TempDir()
	sid := "test-pid-dirs"

	writeCollectorPID(projectDir, sid, 1234)

	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sid)
	info, err := os.Stat(sessDir)
	if err != nil {
		t.Fatalf("session dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("session dir is not a directory")
	}
}

// TestSpawnFailLoudStrict verifies that when HTMLGRAPH_OTEL_STRICT=1 and
// collector spawn fails, spawnSessionCollectorTo emits a FATAL line on the
// provided stderr writer and returns wantExit=true.
func TestSpawnFailLoudStrict(t *testing.T) {
	t.Setenv("HTMLGRAPH_OTEL_STRICT", "1")

	var buf bytes.Buffer
	projectDir := t.TempDir()

	overrides, wantExit := spawnSessionCollectorTo(projectDir, "/nonexistent/binary", &buf)

	stderr := buf.String()
	if !strings.Contains(stderr, "htmlgraph: FATAL:") {
		t.Errorf("expected FATAL line on stderr, got: %q", stderr)
	}
	if !wantExit {
		t.Error("expected wantExit=true when HTMLGRAPH_OTEL_STRICT=1 and spawn fails")
	}
	if overrides.CollectorPort != 0 || overrides.SessionID != "" || overrides.Cleanup != nil {
		t.Errorf("expected zero-value overrides on failure, got: %+v", overrides)
	}
}

// TestSpawnQuietByDefault verifies that without HTMLGRAPH_OTEL_STRICT, a
// failed spawn still emits a FATAL line on stderr but returns wantExit=false
// and zero-value overrides (degraded mode).
func TestSpawnQuietByDefault(t *testing.T) {
	t.Setenv("HTMLGRAPH_OTEL_STRICT", "")

	var buf bytes.Buffer
	projectDir := t.TempDir()

	overrides, wantExit := spawnSessionCollectorTo(projectDir, "/nonexistent/binary", &buf)

	stderr := buf.String()
	if !strings.Contains(stderr, "htmlgraph: FATAL:") {
		t.Errorf("expected FATAL line on stderr even without strict mode, got: %q", stderr)
	}
	if wantExit {
		t.Error("expected wantExit=false when HTMLGRAPH_OTEL_STRICT is not set")
	}
	if overrides.CollectorPort != 0 || overrides.SessionID != "" || overrides.Cleanup != nil {
		t.Errorf("expected zero-value overrides on failure, got: %+v", overrides)
	}
}

// TestRetrySpawn_SucceedsOnThirdAttempt injects a fake spawn function that
// fails on attempts 1 and 2 then succeeds on attempt 3. Verifies the final
// return values are from the successful attempt and that two warning lines
// were written to stderr.
func TestRetrySpawn_SucceedsOnThirdAttempt(t *testing.T) {
	callCount := 0
	var buf bytes.Buffer

	fakeFn := func(binPath, sessionID, projectDir string) (int, *os.Process, error) {
		callCount++
		if callCount < 3 {
			return 0, nil, fmt.Errorf("transient error attempt %d", callCount)
		}
		return 9999, &os.Process{Pid: 12345}, nil
	}

	port, proc, attempts, err := retrySpawnCollector("/fake/bin", "sid", t.TempDir(), 3, fakeFn, &buf)

	if err != nil {
		t.Fatalf("expected success on third attempt, got error: %v", err)
	}
	if port != 9999 {
		t.Errorf("port = %d, want 9999", port)
	}
	if proc == nil || proc.Pid != 12345 {
		t.Errorf("unexpected proc: %+v", proc)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	stderr := buf.String()
	warnCount := strings.Count(stderr, "htmlgraph: warning: collector spawn attempt")
	if warnCount != 2 {
		t.Errorf("expected 2 warning lines, got %d; stderr=%q", warnCount, stderr)
	}
}

// TestRetrySpawn_AllFail injects a fake spawn function that always fails.
// Verifies the error is returned, attempts==3, and 2 warning lines appear
// (warning for attempts 1 and 2; attempt 3 failure is surfaced as the error).
func TestRetrySpawn_AllFail(t *testing.T) {
	callCount := 0
	var buf bytes.Buffer

	fakeFn := func(binPath, sessionID, projectDir string) (int, *os.Process, error) {
		callCount++
		return 0, nil, fmt.Errorf("persistent failure attempt %d", callCount)
	}

	port, proc, attempts, err := retrySpawnCollector("/fake/bin", "sid", t.TempDir(), 3, fakeFn, &buf)

	if err == nil {
		t.Fatal("expected error when all attempts fail, got nil")
	}
	if port != 0 {
		t.Errorf("port = %d, want 0", port)
	}
	if proc != nil {
		t.Error("expected nil process on failure")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	stderr := buf.String()
	warnCount := strings.Count(stderr, "htmlgraph: warning: collector spawn attempt")
	if warnCount != 2 {
		t.Errorf("expected 2 warning lines, got %d; stderr=%q", warnCount, stderr)
	}
}

// TestSpawnSessionCollectorTo_RetriesOnTransientFailure verifies that the
// higher-level spawnSessionCollectorTo succeeds when the underlying spawn
// fails on the first attempt but succeeds on the second.
func TestSpawnSessionCollectorTo_RetriesOnTransientFailure(t *testing.T) {
	t.Setenv("HTMLGRAPH_OTEL_STRICT", "")

	callCount := 0
	origFn := spawnCollectorFn
	t.Cleanup(func() { spawnCollectorFn = origFn })

	spawnCollectorFn = func(binPath, sessionID, projectDir string) (int, *os.Process, error) {
		callCount++
		if callCount < 2 {
			return 0, nil, fmt.Errorf("transient error")
		}
		return 8888, &os.Process{Pid: 99999}, nil
	}

	var buf bytes.Buffer
	projectDir := t.TempDir()

	overrides, wantExit := spawnSessionCollectorTo(projectDir, "/fake/bin", &buf)

	if wantExit {
		t.Error("expected wantExit=false on eventual success")
	}
	if overrides.CollectorPort != 8888 {
		t.Errorf("CollectorPort = %d, want 8888", overrides.CollectorPort)
	}
	if overrides.SessionID == "" {
		t.Error("expected non-empty SessionID")
	}
	if overrides.Cleanup == nil {
		t.Error("expected non-nil Cleanup")
	}
	if callCount != 2 {
		t.Errorf("callCount = %d, want 2", callCount)
	}
}
