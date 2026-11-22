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
	writeCollectorPIDFile(projectDir, sessionID, pid, port, false, SessionExport{})
}

// WriteSharedCollectorPID records that a session exports to the project's
// shared collector. The trailing "shared" line tells the SessionEnd hook not
// to signal it: other sessions still use it.
func WriteSharedCollectorPID(projectDir, sessionID string, pid, port int) {
	writeCollectorPIDFile(projectDir, sessionID, pid, port, true, SessionExport{})
}

// SessionExport is what a launcher records about its own session: the port
// its harness exports to (fixed at launch), the launcher pid, and the harness.
// A harness cannot change its exporter endpoint while running, so doctor
// compares Port with the shared collector's current port.
type SessionExport struct {
	Port     int
	Launcher int
	Harness  string
}

// WriteSharedSessionPID is WriteSharedCollectorPID that also records exp as
// "export_port=", "launcher=" and "harness=" lines. Zero fields are omitted.
func WriteSharedSessionPID(projectDir, sessionID string, pid, port int, exp SessionExport) {
	writeCollectorPIDFile(projectDir, sessionID, pid, port, true, exp)
}

func writeCollectorPIDFile(projectDir, sessionID string, pid, port int, shared bool, exp SessionExport) {
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
	if exp.Port > 0 {
		content += "export_port=" + strconv.Itoa(exp.Port) + "\n"
	}
	if exp.Launcher > 0 {
		content += "launcher=" + strconv.Itoa(exp.Launcher) + "\n"
	}
	if exp.Harness != "" {
		content += "harness=" + exp.Harness + "\n"
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

// ReadSessionExport returns the export record in a .collector-pid file. Fields
// are zero when the file is missing or predates them.
func ReadSessionExport(pidPath string) SessionExport {
	var exp SessionExport
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return exp
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "export_port="); ok {
			exp.Port, _ = strconv.Atoi(v)
		} else if v, ok := strings.CutPrefix(line, "launcher="); ok {
			exp.Launcher, _ = strconv.Atoi(v)
		} else if v, ok := strings.CutPrefix(line, "harness="); ok {
			exp.Harness = v
		}
	}
	return exp
}

// MovedPortAdvice is the recovery sentence for a session whose collector
// moved. An unknown harness names both resume commands.
func MovedPortAdvice(harness string) string {
	cmd := "`wipnote claude --continue` (or `wipnote codex --continue` for Codex)"
	switch harness {
	case "claude":
		cmd = "`wipnote claude --continue`"
	case "codex":
		cmd = "`wipnote codex --continue`"
	}
	return "telemetry from this session may be lost; exit and relaunch with " + cmd
}
