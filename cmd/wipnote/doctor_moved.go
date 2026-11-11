package main

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// writeMovedSessions lists each live session whose launch-time export port
// differs from the shared collector's current port, with the recovery step.
// It only reads files and checks pids; it returns how many it listed. Nothing
// is printed when no lease names a live collector.
func writeMovedSessions(w io.Writer, root string) int {
	lease, err := collector.ReadSharedRecord(root)
	if err != nil || !collector.PIDAlive(lease.PID) {
		return 0
	}
	files, _ := filepath.Glob(filepath.Join(root, ".wipnote", "sessions", "*", ".collector-pid"))
	sort.Strings(files)
	n := 0
	for _, f := range files {
		exp := collector.ReadSessionExport(f)
		if exp.Port == 0 || exp.Port == lease.Port || exp.Launcher == 0 || !collector.PIDAlive(exp.Launcher) {
			continue
		}
		if n == 0 {
			fmt.Fprintln(w, "\nsessions exporting to a port the collector left")
		}
		n++
		fmt.Fprintf(w, "  session %s (launcher pid %d) exports to port %d; the collector is on port %d: %s\n",
			filepath.Base(filepath.Dir(f)), exp.Launcher, exp.Port, lease.Port, collector.MovedPortAdvice(exp.Harness))
	}
	return n
}
