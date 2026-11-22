package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/bgproc"
)

// doctorProcessesLong is the --processes half of `wipnote doctor`'s help.
const doctorProcessesLong = `--processes lists this project's wipnote background processes: the headless
writer daemon (_serve-child) and the otel-collect collectors. It probes each:
the writer's read socket, a collector's /healthz. It also lists live sessions whose export port is no
longer the shared collector's port. It only reports.

--restart-all also stops each hung or orphaned one: SIGTERM, then SIGKILL
after a grace period, and starts a fresh writer if it stopped one. Launchers
start replacement collectors on their own. It acts only on a process that a
wipnote lease or PID file names AND whose argv is a wipnote background command
for this project. It never touches launchers, Herdr or anything else.`

// doctorInspectOpts is the bgproc configuration; tests replace it.
var doctorInspectOpts = func() bgproc.Options {
	return bgproc.Options{RestartWriter: restartWriterDaemon}
}

// restartWriterDaemon starts a writer for root the way a launcher does.
func restartWriterDaemon(ctx context.Context, root string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	_, err = ensureDaemonFn(ctx, root, exe, 0)
	return err
}

// runDoctorProcesses reports the project's background processes and, with
// restart, stops the hung or orphaned ones.
func runDoctorProcesses(ctx context.Context, w io.Writer, restart bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	root := filepath.Dir(wipnoteDir)
	opts := doctorInspectOpts()
	procs, err := bgproc.Inspect(ctx, root, opts)
	if err != nil {
		return fmt.Errorf("list processes: %w", err)
	}
	stoppable := writeProcessReport(w, root, procs)
	writeMovedSessions(w, root)
	if !restart {
		if stoppable > 0 {
			fmt.Fprintf(w, "\n%d hung or orphaned. Run `wipnote doctor --processes --restart-all` to stop them.\n", stoppable)
		}
		return nil
	}
	return writeRestartResults(w, bgproc.Restart(ctx, root, procs, opts))
}

// writeProcessReport prints one line per process and returns how many
// Restart would stop.
func writeProcessReport(w io.Writer, root string, procs []bgproc.Proc) int {
	fmt.Fprintf(w, "wipnote background processes — %s\n", root)
	if len(procs) == 0 {
		fmt.Fprintln(w, "  none recorded")
		return 0
	}
	stoppable := 0
	for _, p := range procs {
		if p.Stoppable() {
			stoppable++
		}
		detail := ""
		if p.Port > 0 {
			detail += fmt.Sprintf(" port %d", p.Port)
		}
		if p.Session != "" && p.Kind == bgproc.KindSessionCollector {
			detail += " session " + p.Session
		}
		fmt.Fprintf(w, "  %-10s %-12s pid %d%s — %s\n", p.State, p.Kind, p.PID, detail, p.Reason)
	}
	return stoppable
}

// writeRestartResults prints what Restart did; it returns an error when a
// stop or the writer restart failed.
func writeRestartResults(w io.Writer, results []bgproc.StopResult) error {
	if len(results) == 0 {
		fmt.Fprintln(w, "\nnothing to stop")
		return nil
	}
	failed := 0
	fmt.Fprintln(w)
	for _, r := range results {
		switch {
		case errors.Is(r.Err, bgproc.ErrNoLongerOurs):
			fmt.Fprintf(w, "  skipped pid %d: %v\n", r.Proc.PID, r.Err)
		case r.Err != nil:
			failed++
			fmt.Fprintf(w, "  FAILED  %s pid %d: %v\n", r.Proc.Kind, r.Proc.PID, r.Err)
		case r.Killed:
			fmt.Fprintf(w, "  stopped %s pid %d (SIGKILL after SIGTERM was ignored)\n", r.Proc.Kind, r.Proc.PID)
		default:
			fmt.Fprintf(w, "  stopped %s pid %d (SIGTERM)\n", r.Proc.Kind, r.Proc.PID)
		}
	}
	if failed > 0 {
		return fmt.Errorf("doctor --restart-all: %d failed", failed)
	}
	return nil
}
