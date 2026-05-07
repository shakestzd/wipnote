package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// serveLockPath returns the path of the per-project serve lock file.
// The lock file stores the PID of a running `wipnote serve` process.
func serveLockPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", ".serve.lock")
}

// ensureServeForDashboard spawns a detached `wipnote serve` if one is not
// already running. Called from launchClaude before exec'ing claude so that
// the dashboard (and semantic-ops such as AI-title backfill) are available
// for the duration of the claude session. Serve is no longer auto-started
// for telemetry purposes — per-session collectors handle OTLP ingest.
//
// Gating:
//   - When WIPNOTE_OTEL_ENABLED is explicitly disabled (0/false/no/off),
//     return immediately — user opted out of the full wipnote stack.
//   - When the dashboard port (8080) already accepts a TCP connection,
//     a serve process is assumed live — return nil.
//   - Otherwise spawn `wipnote serve` detached, wait up to 3 seconds
//     for it to bind port 8080, and log a warning if it never does. Never
//     return an error — a missing dashboard is degraded operation, not a
//     fatal launcher failure.
//
// Stdout/stderr go to a log file under .wipnote/logs so the orphaned
// server doesn't pollute the user's terminal.
func ensureServeForDashboard(projectDir string) {
	if isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_ENABLED")) {
		return
	}

	// Probe the dashboard port (8080) rather than the OTLP port — serve
	// is now a pure reader + dashboard server, not a receiver.
	const dashboardHost = "127.0.0.1"
	const dashboardPort = 8080

	if probePort(dashboardHost, dashboardPort, 200*time.Millisecond) {
		return // something is already bound — leave it alone
	}

	// Check the lockfile before spawning. If a serve process is already
	// running (lock file contains a live PID), skip the spawn to prevent
	// a second wipnote serve from racing to bind port 8080.
	if skipSpawn, stale := checkServeLock(projectDir); skipSpawn {
		debugLog("ensureServeForDashboard: skipping spawn, serve already running (lockfile)")
		return
	} else if stale {
		// Stale lockfile (process gone) — remove it so future spawns work.
		_ = os.Remove(serveLockPath(projectDir))
	}

	if err := spawnDetachedServe(projectDir); err != nil {
		fmt.Fprintf(os.Stderr, "wipnote: auto-start serve failed: %v\n", err)
		return
	}

	// Poll the dashboard port for up to 3 seconds.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		if probePort(dashboardHost, dashboardPort, 200*time.Millisecond) {
			fmt.Fprintf(os.Stderr, "wipnote: started serve (dashboard) on %s:%d\n", dashboardHost, dashboardPort)
			return
		}
		select {
		case <-ctx.Done():
			fmt.Fprintf(os.Stderr, "wipnote: serve did not bind %s:%d within 3s; dashboard may be unavailable\n", dashboardHost, dashboardPort)
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// probePort returns true when host:port accepts a TCP connection within
// the given timeout. Used both to detect an existing receiver and to
// wait for a freshly-spawned one to come up.
func probePort(host string, port int, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, strconv.Itoa(port)), timeout)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// otelNoticeMarkerPath returns the path of the one-time notice marker file.
func otelNoticeMarkerPath(projectDir string) string {
	return filepath.Join(projectDir, ".wipnote", ".otel-notice-shown")
}

// MaybeShowOtelNotice prints a one-time notice to STDERR on first launch
// explaining that wipnote captures Claude Code telemetry via OTel.
// Subsequent launches are silent (a marker file records that the notice
// has been shown). Safe to call when .wipnote/ doesn't exist — it
// simply returns without creating the directory or printing anything.
func MaybeShowOtelNotice(projectDir string) {
	if projectDir == "" {
		return
	}
	// Respect explicit opt-out — no need to explain what we're not doing.
	if isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_ENABLED")) {
		return
	}
	// Only print when .wipnote/ already exists — don't create it just
	// to write the marker.
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if _, err := os.Stat(wipnoteDir); os.IsNotExist(err) {
		return
	}
	markerPath := otelNoticeMarkerPath(projectDir)
	if _, err := os.Stat(markerPath); err == nil {
		return // notice already shown on a previous launch
	}

	notice := strings.Join([]string{
		"",
		"  wipnote: OTel telemetry is on (first-launch notice)",
		"  -------------------------------------------------------",
		"  wipnote auto-captures Claude Code activity via OpenTelemetry:",
		"    tool calls, prompts, costs, token usage, and latencies.",
		"",
		"  A per-session OTLP collector is started automatically.",
		"  Data stays 100% local, stored in .wipnote/wipnote.db.",
		"",
		"  Powers: activity feed · per-turn cost badges · span timeline",
		"  Opt out: set WIPNOTE_OTEL_ENABLED=0 before launching.",
		"",
	}, "\n")
	fmt.Fprint(os.Stderr, notice)

	// Write marker so the notice doesn't repeat. Ignore errors — if the
	// write fails, re-showing the notice next launch is acceptable.
	_ = os.WriteFile(markerPath, []byte("shown\n"), 0o644)
}

// checkServeLock reads the per-project serve lock file and checks whether
// the PID it contains refers to a live process.
//
// Returns (skipSpawn=true, stale=false) when the lock exists and is alive.
// Returns (skipSpawn=false, stale=true) when the lock exists but the PID is dead.
// Returns (skipSpawn=false, stale=false) when the lock does not exist.
func checkServeLock(projectDir string) (skipSpawn, stale bool) {
	data, err := os.ReadFile(serveLockPath(projectDir))
	if err != nil {
		return false, false // no lock file
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return false, true // malformed — treat as stale
	}
	// kill -0 checks process existence without sending a signal.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false, true // can't find process — stale
	}
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return false, true // process not alive — stale
	}
	return true, false // process alive — skip spawn
}

// writeServeLock writes the current process PID to the per-project serve
// lock file. Called by `wipnote serve` on startup so concurrent launchers
// can detect a live serve process and skip spawning a duplicate.
// The write is best-effort — errors are silently ignored because a missing
// lock file causes a harmless duplicate-spawn attempt (which then fails to
// bind and exits cleanly).
func writeServeLock(projectDir string) {
	lockPath := serveLockPath(projectDir)
	_ = os.WriteFile(lockPath, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// removeServeLock removes the per-project serve lock file on graceful
// shutdown so subsequent launcher invocations don't see a stale lock.
func removeServeLock(projectDir string) {
	_ = os.Remove(serveLockPath(projectDir))
}

// debugLog writes a message to stderr only when WIPNOTE_DEBUG is set.
// Used for low-level operational tracing that should not appear in normal output.
func debugLog(msg string) {
	if os.Getenv("WIPNOTE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "wipnote [debug]: %s\n", msg)
	}
}

// spawnDetachedServe starts `wipnote serve` in a new process group so
// it survives the launcher's exit and keeps serving the dashboard (and
// the OTel receiver) after claude terminates. Output redirects to
// .wipnote/logs/serve-auto.log.
//
// Uses os.Executable() for the binary path so the spawned server is
// the SAME version as the launcher — prevents version skew when the
// user has multiple wipnote builds on PATH (dev vs released).
func spawnDetachedServe(projectDir string) error {
	binPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self path: %w", err)
	}
	logDir := filepath.Join(projectDir, ".wipnote", "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "serve-auto.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}

	cmd := exec.Command(binPath, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.Dir = projectDir
	// Detach from the launcher's process group so the server survives
	// our exit. macOS and Linux both accept Setpgid via SysProcAttr.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Inherit env including WIPNOTE_OTEL_ENABLED so the child's
	// serve_child turns the receiver on.
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("spawn serve: %w", err)
	}
	// Don't Wait — let it run. Log file close happens implicitly at
	// process exit; our close is fine to defer since the child has its
	// own fd after Start().
	_ = logFile.Close()
	return nil
}
