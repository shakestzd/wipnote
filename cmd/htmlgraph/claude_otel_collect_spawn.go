package main

import (
	"bufio"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// generateOtelSessionID produces a hex session ID from a Unix-millisecond
// timestamp (12 hex digits) and 8 random bytes (16 hex digits), giving
// 28 hex characters total. Lexicographically sortable by creation time.
// Distinct from generateSessionID (sess-{hex8}) which is used for
// non-OTel session tracking.
func generateOtelSessionID() string {
	ts := time.Now().UnixMilli()
	var entropy [8]byte
	_, _ = rand.Read(entropy[:]) // crypto/rand never errors on supported platforms
	return fmt.Sprintf("%012x%016x", ts, entropy)
}

// spawnCollector starts an otel-collect child process, waits for its
// handshake line ("htmlgraph-otel-ready port=<N>"), and returns the
// port and process. The child is started in its own process group
// (Setpgid) so it can be independently signalled.
//
// binPath is the path to the htmlgraph binary to invoke. In production
// callers should pass the result of os.Executable(); tests pass a
// pre-built test binary.
func spawnCollector(binPath, sessionID, projectDir string) (int, *os.Process, error) {
	cmd := exec.Command(binPath, "otel-collect",
		"--session-id", sessionID,
		"--project-dir", projectDir,
		"--listen", "127.0.0.1:0",
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return 0, nil, fmt.Errorf("stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return 0, nil, fmt.Errorf("start otel-collect: %w", err)
	}

	port, err := readCollectorHandshake(bufio.NewScanner(stdout))
	if err != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		return 0, nil, err
	}
	return port, cmd.Process, nil
}

// readCollectorHandshake scans stdout for the handshake line within 3s.
func readCollectorHandshake(scanner *bufio.Scanner) (int, error) {
	type result struct {
		port int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			var p int
			if _, err := fmt.Sscanf(line, "htmlgraph-otel-ready port=%d", &p); err == nil {
				ch <- result{port: p}
				return
			}
		}
		ch <- result{err: fmt.Errorf("otel-collect: handshake not found (stdout closed)")}
	}()

	select {
	case r := <-ch:
		return r.port, r.err
	case <-time.After(3 * time.Second):
		return 0, fmt.Errorf("otel-collect: handshake timeout (3s)")
	}
}

// otelEnvOverrides holds optional overrides for OTel env vars set by
// the launcher. Zero-value fields mean "use the default derivation".
type otelEnvOverrides struct {
	CollectorPort int
	SessionID     string
	Cleanup       func() // called on launcher exit to SIGTERM the collector
}

// spawnSessionCollectorTo is the testable core of collector spawning.
// It generates a session ID, spawns the collector at binPath, and returns
// overrides and a wantExit flag. On spawn failure it always writes a FATAL
// line to errW; wantExit is true only when HTMLGRAPH_OTEL_STRICT=1.
// Silent-fail is preserved for soft-precondition failures that occur before
// spawn (binary path resolution) — those are handled by the caller.
func spawnSessionCollectorTo(projectDir, binPath string, errW io.Writer) (otelEnvOverrides, bool) {
	sessionID := generateOtelSessionID()

	port, proc, err := spawnCollector(binPath, sessionID, projectDir)
	if err != nil {
		fmt.Fprintf(errW, "htmlgraph: FATAL: collector spawn failed after all retries: %v\n", err)
		wantExit := os.Getenv("HTMLGRAPH_OTEL_STRICT") == "1"
		return otelEnvOverrides{}, wantExit
	}

	writeCollectorPID(projectDir, sessionID, proc.Pid)
	cleanup := registerCollectorCleanup(proc, projectDir, sessionID)

	return otelEnvOverrides{
		CollectorPort: port,
		SessionID:     sessionID,
		Cleanup:       cleanup,
	}, false
}

// spawnSessionCollector generates a session ID, spawns a per-session
// collector, writes the PID file, and returns a cleanup function.
// On spawn failure emits a FATAL line to stderr; exits non-zero when
// HTMLGRAPH_OTEL_STRICT=1. Silent-fail is preserved when the binary
// path cannot be resolved (soft precondition).
func spawnSessionCollector(projectDir string) otelEnvOverrides {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "htmlgraph: warning: per-session collector skipped: %v\n", err)
		return otelEnvOverrides{}
	}

	overrides, wantExit := spawnSessionCollectorTo(projectDir, binPath, os.Stderr)
	if wantExit {
		os.Exit(1)
	}
	return overrides
}

// registerCollectorCleanup spawns a reaper goroutine for the collector
// child so it doesn't become a zombie if it exits on its own (idle
// timeout). Returns a cleanup function that sends SIGTERM, waits, and
// removes the .collector-pid file so subsequent liveness probes by
// /api/otel/status do not see a stale PID after process exit.
func registerCollectorCleanup(proc *os.Process, projectDir, sessionID string) func() {
	go func() { _, _ = proc.Wait() }()

	return func() {
		_ = proc.Signal(syscall.SIGTERM)
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				removeCollectorPID(projectDir, sessionID)
				return // process exited
			}
			time.Sleep(100 * time.Millisecond)
		}
		_ = proc.Kill()
		removeCollectorPID(projectDir, sessionID)
	}
}

// removeCollectorPID removes the .collector-pid file for a session.
// Best-effort: missing file or unreadable directory is not an error.
func removeCollectorPID(projectDir, sessionID string) {
	pidPath := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID, ".collector-pid")
	_ = os.Remove(pidPath)
}

// writeCollectorPID writes the collector PID to the session directory.
// Best-effort: errors are silently ignored (the PID file is used by
// the SessionEnd hook as a hint; its absence is not fatal).
func writeCollectorPID(projectDir, sessionID string, pid int) {
	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID)
	_ = os.MkdirAll(sessDir, 0o755)
	pidPath := filepath.Join(sessDir, ".collector-pid")
	_ = os.WriteFile(pidPath, []byte(strconv.Itoa(pid)+"\n"), 0o644)
}
