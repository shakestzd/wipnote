package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

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
