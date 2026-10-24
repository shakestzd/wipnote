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

// collectorDrainWait bounds how long SessionEnd waits for the collector to
// drain. Codex caps a SessionEnd hook at 3s (default 1s;
// codex-rs hooks/src/events/session_end.rs) and kills it past that, so the
// wait shrinks there to leave headroom for the telemetry finalize; Claude Code
// grants SessionEnd the hook's configured timeout (60s in hooks.json).
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
// for a clean drain, then falls back to SIGKILL.
// All errors are silently logged — the collector PID file is best-effort.
func signalCollector(projectDir, sessionID string) {
	pidPath := filepath.Join(projectDir, ".wipnote", "sessions", sessionID, ".collector-pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// No PID file — collector was never spawned or already cleaned up.
		return
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		debugLog(projectDir, "[session-end] collector-pid: invalid pid %q for session %s", pidStr, sessionID[:minLen(sessionID, 8)])
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

	if !waitForExit(proc, collectorDrainWait()) {
		debugLog(projectDir, "[session-end] collector drain timeout — sending SIGKILL pid=%d", pid)
		_ = proc.Signal(syscall.SIGKILL)
	}

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
