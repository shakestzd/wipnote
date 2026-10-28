package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/spf13/cobra"
)

// reconcileCmd is `wipnote reconcile [--strict]`. It runs a session-exit
// reconciliation pass over the current project and reports three classes of
// drift:
//
//  1. done-but-uncommitted — work items in a terminal state whose canonical
//     artifact is dirty in git. These are AUTO-COMMITTED (deterministic
//     bookkeeping: the "done" decision was already made; we only persist the
//     durable record) and reported.
//  2. generator-touched-without-build-ports — reuses slice-2's
//     `wipnote plugin check-ports` engine (port/pluginbuild.CheckPorts)
//     verbatim; NOT reimplemented here.
//  3. started-but-orphaned — in-progress items with no live owning session.
//     Reported only; never auto-resolved.
//  4. orphaned processes — live processes whose agent session has ended,
//     found by the session id they inherited (internal/procsweep). Reported
//     with a stop command to copy; wipnote never stops a process. This class
//     is CLI-only: hooks.Reconcile, which the session-end hook runs, never
//     sweeps processes.
//
// --processes prints only the process view — every live process this
// project's agent sessions started, live or orphaned — and skips the item
// classes, so it is read-only (no auto-commit).
//
// Without --strict the command always exits 0 (report-only). With --strict it
// exits non-zero when ambiguous, human-resolvable drift remains (generator
// drift): the auto-commit and orphan classes are deterministic / informational
// and never fail the strict gate by themselves.
func reconcileCmd() *cobra.Command {
	var strict, processes bool
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Reconcile session-exit drift (uncommitted artifacts, port drift, orphans)",
		Long: "Run a session-exit reconciliation pass. Auto-commits done-but-" +
			"uncommitted work-item artifacts, reports generator drift (reusing " +
			"`wipnote plugin check-ports`), and reports started-but-orphaned " +
			"items and processes that outlived their session. With --strict, " +
			"exits non-zero when ambiguous generator drift remains so a human " +
			"reconciles it before proceeding. --processes lists every live " +
			"process this project's agent sessions started, read-only.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			projectDir := filepath.Dir(wipnoteDir)
			out := cmd.OutOrStdout()
			if processes {
				res, err := sweepProjectProcesses(projectDir)
				printSessionProcesses(out, res, err)
				return nil
			}
			return runReconcileReport(cmd, out, projectDir, strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false,
		"exit non-zero when ambiguous generator drift remains")
	cmd.Flags().BoolVar(&processes, "processes", false,
		"list live processes started by this project's agent sessions (read-only)")
	return cmd
}

// runReconcileReport is the default reconcile pass: the three item classes
// from hooks.Reconcile plus the orphaned-process class.
func runReconcileReport(cmd *cobra.Command, out io.Writer, projectDir string, strict bool) error {
	// No handle to open: all three classes read canonical state.
	// This used to open a project read index it never passed on —
	// hooks.Reconcile was called with a hardcoded nil, and the two
	// item-shaped classes were gated behind `database != nil`, so
	// the report was permanently "nothing to reconcile" no matter
	// how much done-but-uncommitted work existed (feat-fc3cc9e0).
	rep, err := hooks.Reconcile(projectDir, strict)
	if err != nil {
		return err
	}
	for _, id := range rep.AutoCommitted {
		fmt.Fprintf(out, "reconcile: auto-committed artifact for %s (was done but uncommitted)\n", id)
	}
	for _, pth := range rep.PortDrift {
		fmt.Fprintf(out, "reconcile: generator drift — %s (run `wipnote plugin build-ports` and commit)\n", pth)
	}
	for _, id := range rep.Orphaned {
		fmt.Fprintf(out, "reconcile: orphaned in-progress item %s (no live owning session)\n", id)
	}
	res, sweepErr := sweepProjectProcesses(projectDir)
	wroteProcs := printOrphanProcesses(out, res, sweepErr)
	if rep.Empty() && !wroteProcs {
		fmt.Fprintln(out, "reconcile: nothing to reconcile")
	}

	if strict && rep.HasAmbiguousDrift() {
		cmd.SilenceUsage = true
		cmd.SilenceErrors = true
		return fmt.Errorf("reconcile --strict: %d unresolved generator-drift path(s)", len(rep.PortDrift))
	}
	return nil
}
