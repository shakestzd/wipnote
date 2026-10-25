package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
)

// collectorDrainWait bounds how long a CATCH-UP pass waits for a collector to
// drain before it sends SIGKILL. The SessionEnd hook itself no longer waits.
//
// Claude Code does NOT grant a plugin SessionEnd hook its configured timeout.
// The hooks reference (https://code.claude.com/docs/en/hooks, SessionEnd) says:
// "SessionEnd hooks have a default timeout of 1.5 seconds" and "Timeouts set on
// plugin-provided hooks don't raise the budget". The "timeout": 60 in
// plugin/hooks/hooks.json is therefore ignored by Claude Code and stays only for
// harnesses that read it. Codex caps a SessionEnd hook at 3s (default 1s;
// codex-rs hooks/src/events/session_end.rs) and kills it past that.
func collectorDrainWait() time.Duration {
	if agent.DetectEnvHarness() == agent.HarnessCodex {
		return 1200 * time.Millisecond
	}
	return 3 * time.Second
}

// telemetryFinalizeBudget bounds SessionTelemetryFn: ingesting the session's
// NDJSON and materializing its rollup. Under Codex, drain plus finalize stays
// near 2.1s of the 3s cap, leaving room for the steps before and after them.
// A session too large for the budget is not lost: it stays unmarked, and the
// catch-up pass before the next retention sweep finishes it.
func telemetryFinalizeBudget() time.Duration {
	if agent.DetectEnvHarness() == agent.HarnessCodex {
		return 900 * time.Millisecond
	}
	return 10 * time.Second
}

// signalCollector reads the .collector-pid file for this session, sends SIGTERM,
// waits up to the collector drain budget (collectorDrainWait; 3s outside Codex)
// for a clean drain, then falls back to SIGKILL. The SessionEnd hook does not
// call it (it has 1.5s); see termCollector. Catch-up uses the Wait variant.
// All errors are silently logged — the collector PID file is best-effort.
func signalCollector(projectDir, sessionID string) {
	// The launcher keys the collector's directory by its own OTel id
	// (WIPNOTE_OTEL_SESSION_ID), not the harness session id the hook receives.
	// Try the launcher id first, then the harness id as the fallback.
	launcher := os.Getenv("WIPNOTE_OTEL_SESSION_ID")
	if launcher != "" && launcher != sessionID && filepath.Base(launcher) == launcher && launcher != ".." && launcher != "." {
		signalCollectorDir(projectDir, launcher)
	}
	signalCollectorDir(projectDir, sessionID)
}

// collectorPID extracts the collector pid from the PID file contents. The
// writer records "<pid>\n<starttime>". When CollectorAliveFn is wired it also
// proves the pid is still the same collector, so a reused pid is never signalled.
func collectorPID(sessDir string, data []byte) (int, bool) {
	if CollectorAliveFn != nil {
		alive, pid := CollectorAliveFn(sessDir)
		return pid, alive && pid > 0
	}
	first, _, _ := strings.Cut(strings.TrimSpace(string(data)), "\n")
	pid, err := strconv.Atoi(strings.TrimSpace(first))
	return pid, err == nil && pid > 0
}

// signalCollectorDir signals the collector whose PID file lives under the
// shard directory named sessionID.
func signalCollectorDir(projectDir, sessionID string) {
	signalCollectorDirWait(projectDir, sessionID, collectorDrainWait(), true)
}

// signalCollectorDirWait sends the collector SIGTERM, then waits up to wait for
// it to exit. wait == 0 does not wait at all. When the collector is still alive
// after the wait, kill == true sends SIGKILL; kill == false leaves it running
// and keeps the pid file so a later pass can finish the job. The pid file is
// removed only once the collector is known gone.
func signalCollectorDirWait(projectDir, sessionID string, wait time.Duration, kill bool) {
	pidPath := filepath.Join(projectDir, ".wipnote", "sessions", sessionID, ".collector-pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// No PID file — collector was never spawned or already cleaned up.
		return
	}
	pid, ok := collectorPID(filepath.Dir(pidPath), data)
	if !ok {
		debugLog(projectDir, "[session-end] collector-pid: no live collector in %q for session %s", strings.TrimSpace(string(data)), sessionID[:minLen(sessionID, 8)])
		_ = os.Remove(pidPath)
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		// Process not found (already exited).
		return
	}

	// Send SIGTERM to request graceful drain.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// ESRCH means process already gone — clean up PID file to prevent
		// stale PID reuse on later end/resume paths.
		_ = os.Remove(pidPath)
		return
	}
	debugLog(projectDir, "[session-end] sent SIGTERM to collector pid=%d (session=%s)", pid, sessionID[:minLen(sessionID, 8)])

	if wait > 0 && waitForExit(proc, wait) {
		_ = os.Remove(pidPath)
		return
	}
	if !kill {
		return
	}
	debugLog(projectDir, "[session-end] collector drain timeout — sending SIGKILL pid=%d", pid)
	_ = proc.Signal(syscall.SIGKILL)

	// Remove the PID file so future SessionEnd calls don't attempt to re-signal.
	_ = os.Remove(pidPath)
}

// waitForExit polls proc with kill(pid, 0) — a non-child cannot be Wait()ed —
// and reports whether it exited within drain.
func waitForExit(proc *os.Process, drain time.Duration) bool {
	deadline := time.Now().Add(drain)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return proc.Signal(syscall.Signal(0)) != nil
}
