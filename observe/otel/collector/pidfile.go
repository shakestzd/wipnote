package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// WriteCollectorPID writes the collector PID to the session directory.
// On Linux, also appends the process start time (clock ticks from
// /proc/<pid>/stat field 22) on a second line so future liveness checks
// can detect PID reuse. On non-Linux or when /proc is unreadable, only
// the PID is written; consumers must tolerate single-line files.
//
// Best-effort: errors are silently ignored.
func WriteCollectorPID(projectDir, sessionID string, pid int) {
	WriteCollectorPIDPort(projectDir, sessionID, pid, 0)
}

// WriteCollectorPIDPort is WriteCollectorPID that also records the port the
// collector listens on as a "port=<N>" line, so `wipnote doctor --processes`
// can probe its /healthz. Older readers ignore the extra line. port 0 omits it.
func WriteCollectorPIDPort(projectDir, sessionID string, pid, port int) {
	writeCollectorPIDFile(projectDir, sessionID, pid, port, false)
}

// WriteSharedCollectorPID records that a session exports to the project's
// shared collector. The trailing "shared" line tells the SessionEnd hook not
// to signal it: other sessions still use it.
func WriteSharedCollectorPID(projectDir, sessionID string, pid, port int) {
	writeCollectorPIDFile(projectDir, sessionID, pid, port, true)
}

func writeCollectorPIDFile(projectDir, sessionID string, pid, port int, shared bool) {
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	_ = os.MkdirAll(sessDir, 0o755)
	pidPath := filepath.Join(sessDir, ".collector-pid")
	content := strconv.Itoa(pid) + "\n"
	if start, ok := readProcStartTime(pid); ok {
		content += strconv.FormatUint(start, 10) + "\n"
	}
	if port > 0 {
		content += "port=" + strconv.Itoa(port) + "\n"
	}
	if shared {
		content += "shared\n"
	}
	_ = os.WriteFile(pidPath, []byte(content), 0o644)
}

// ReadCollectorPort returns the port recorded in a .collector-pid file, or 0
// when the file is missing or predates the port line.
func ReadCollectorPort(pidPath string) int {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "port="); ok {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				return n
			}
		}
	}
	return 0
}
