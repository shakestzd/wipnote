package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/shakestzd/wipnote/internal/procsweep"
)

// reconcileProcLister is the process table reconcile sweeps; nil means the
// platform default. Tests swap in a fake table.
var reconcileProcLister procsweep.Lister

// sweepProjectProcesses runs the process sweep against one project's
// sessions ledger. It is CLI-only: hooks.Reconcile (the hook path) never
// calls it, so the sweep never runs in a hook.
func sweepProjectProcesses(projectDir string) (procsweep.Result, error) {
	project := procsweep.Project{
		Name:       filepath.Base(projectDir),
		WipnoteDir: filepath.Join(projectDir, ".wipnote"),
	}
	return procsweep.Sweep(context.Background(), []procsweep.Project{project},
		procsweep.Options{Lister: reconcileProcLister})
}

// printOrphanProcesses writes one reconcile line per orphaned process, or
// the sweep's error (e.g. the platform cannot read environments). It reports
// whether it wrote anything.
func printOrphanProcesses(out io.Writer, res procsweep.Result, err error) bool {
	if err != nil {
		fmt.Fprintf(out, "reconcile: process sweep skipped — %v\n", err)
		return true
	}
	wrote := printProjectErrors(out, "reconcile", res)
	orphans := res.Orphans()
	for _, p := range orphans {
		fmt.Fprintf(out, "reconcile: orphaned process %d — %s (session %s ended; project %s; started %s); stop with: %s\n",
			p.PID, p.Command, p.SessionID, p.Project, procTime(p.StartedAt), p.StopHint)
	}
	return wrote || len(orphans) > 0
}

// printProjectErrors writes one line per project whose sessions ledger the
// sweep could not read (its processes went unattributed), sorted by project.
// It reports whether it wrote anything.
func printProjectErrors(out io.Writer, prefix string, res procsweep.Result) bool {
	names := make([]string, 0, len(res.ProjectErrors))
	for name := range res.ProjectErrors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(out, "%s: project %s skipped — %s\n", prefix, name, res.ProjectErrors[name])
	}
	return len(names) > 0
}

// printSessionProcesses is `reconcile --processes`: every live process that
// an agent session of this project started, orphan or not. Read-only.
func printSessionProcesses(out io.Writer, res procsweep.Result, err error) {
	if err != nil {
		fmt.Fprintf(out, "processes: %v\n", err)
		return
	}
	printProjectErrors(out, "processes", res)
	orphans := len(res.Orphans())
	fmt.Fprintf(out, "processes: %d from agent sessions (%d orphaned)\n", len(res.Procs), orphans)
	if len(res.Procs) > 0 {
		tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "PID\tSTATE\tSESSION\tPROJECT\tSTARTED\tCOMMAND\tSTOP")
		for _, p := range res.Procs {
			state := "live"
			if p.Orphan {
				state = "orphan"
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
				p.PID, state, p.SessionID, p.Project, procTime(p.StartedAt), p.Command, p.StopHint)
		}
		tw.Flush()
	}
	if res.Unattributed > 0 {
		fmt.Fprintf(out, "processes: %d more carry a session id this project's ledger does not know\n", res.Unattributed)
	}
	if orphans > 0 {
		fmt.Fprintln(out, "processes: wipnote never stops a process; copy a STOP command to end one.")
	}
}

func procTime(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	return t.Local().Format("2006-01-02 15:04")
}
