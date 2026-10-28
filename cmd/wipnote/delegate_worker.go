package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateWorkerOptions make a claude -p run inside herdr an interactive
// worker (plan-28b4a7f6 slice 22): the boot line spawn types, the status
// line command its --settings get, and the session deps spawn uses.
func delegateWorkerOptions(wipnoteDir, caller string) *oneshot.WorkerOptions {
	projectDir := filepath.Dir(wipnoteDir)
	exe, err := os.Executable()
	if err != nil {
		exe = "wipnote"
	}
	return &oneshot.WorkerOptions{
		Deps: session.Deps{Claims: claimLedgerStore(wipnoteDir), ExpireClaims: expireDelegateWorkerClaims(wipnoteDir),
			ResolveWorkItem: func(id string) (string, error) { return resolveSpawnWorkItem(wipnoteDir, id) }},
		BootLine:   func(run string) string { return session.BootLine(exe, projectDir, run, caller) },
		StatusLine: func(run string) string { return delegateStatusLineCommand(exe, projectDir, run) },
	}
}

// delegateStatusLineCommand is the shell command Claude Code runs as the
// worker's status line.
func delegateStatusLineCommand(exe, projectDir, run string) string {
	return session.Quote(exe) + " delegate status-line --project-dir " + session.Quote(projectDir) + " " + session.Quote(run)
}

// delegateStatusLineCmd is a one-shot worker's status line command: Claude
// Code pipes it the session's cost and token totals on each refresh, and it
// saves them in the run folder for the report and the budget cap. It prints
// the usage line. Any problem prints nothing and exits 0: a status line
// must never break the worker.
func delegateStatusLineCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "status-line <run-id>",
		Short:  "Record a delegate worker's cost and tokens from Claude's status line (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if line := recordDelegateStatus(args[0], cmd); line != "" {
				fmt.Fprintln(cmd.OutOrStdout(), "delegate "+line)
			}
			return nil
		},
	}
}

func recordDelegateStatus(run string, cmd *cobra.Command) string {
	wipnoteDir, err := findWipnoteDir()
	if err != nil || !runstore.ValidRunID(run) {
		return ""
	}
	store, err := runstore.New(filepath.Dir(wipnoteDir))
	if err != nil {
		return ""
	}
	rf, err := store.Run(run)
	if err != nil {
		return ""
	}
	line, _ := oneshot.RecordStatusLine(rf, cmd.InOrStdin())
	return line
}
