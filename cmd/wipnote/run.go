package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/internal/gate"
)

// runCmdExit ends the process with the command's exit status; tests replace it.
var runCmdExit = os.Exit

// wrapperRunCmd is `wipnote run [--work-item ID] [--description D] -- <cmd>`
// (feat-36658c22): the optional wrapper for a script run outside a harness.
// Inside Claude Code or Codex, background tasks are recorded from the
// harness's own records and need no wrapper.
func wrapperRunCmd() *cobra.Command {
	var workItem, description string
	cmd := &cobra.Command{
		Use:   "run [--work-item ID] [--description D] -- <command> [args...]",
		Short: "Run a command and record it as a run on the work item",
		Long: `Runs one command, passing its output through, and records a run in
.wipnote/runs/: start and end time, folder, commit, exit code, the output
log (.wipnote/logs/runs/<run-id>.log, gitignored), its last line and its first
error line. Exits with the command's own status (127 when it cannot start).

The work item defaults to the caller's active claim. Shown by
'wipnote lineage <id>'. Agents' background tasks in Claude Code and Codex are
recorded without this wrapper.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runWrapped(cmd, args, workItem, description)
			if err != nil {
				return err
			}
			if code != 0 {
				runCmdExit(code)
			}
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false) // the command's own flags are never wipnote's
	cmd.Flags().StringVar(&workItem, "work-item", "", "work item id (default: the caller's active claim)")
	cmd.Flags().StringVar(&description, "description", "", "short name shown in lineage (default: the command)")
	return cmd
}

func runWrapped(cmd *cobra.Command, argv []string, workItem, description string) (int, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return 0, err
	}
	projectDir := filepath.Dir(wipnoteDir)
	start := wrapperStart(projectDir, argv, workItem, description)
	if err := start.Validate(); err != nil {
		return 0, err
	}
	store := runledger.NewStore(wipnoteDir)
	if err := store.Append(start); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "wipnote run: run not recorded: %v\n", err)
	}
	logAbs := filepath.Join(projectDir, filepath.FromSlash(start.LogPath))
	code, status := execTee(cmd.Context(), argv, logAbs, cmd.OutOrStdout(), cmd.ErrOrStderr())
	end := runledger.Event{
		RunID: start.RunID, Phase: runledger.PhaseEnd, At: time.Now().UTC(), Kind: start.Kind,
		Status: status, Exit: &code, Session: start.Session, Harness: start.Harness,
	}
	last, first := runledger.SummarizeFile(logAbs)
	end.LastLine, end.FirstError = runledger.Clean(last, projectDir), runledger.Clean(first, projectDir)
	if err := store.Append(end); err != nil {
		fmt.Fprintf(cmd.ErrOrStderr(), "wipnote run: end not recorded: %v\n", err)
	}
	return code, nil
}

// wrapperStart builds the start event; the work item defaults to the active
// claim of the calling session.
func wrapperStart(projectDir string, argv []string, workItem, description string) runledger.Event {
	session := hooks.EnvSessionID("")
	if workItem == "" {
		workItem = gate.ActiveWorkItemForGate(projectDir, session, db.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID")))
	}
	cwd, _ := os.Getwd()
	runID := runledger.NewRunID()
	return runledger.Event{
		RunID: runID, Phase: runledger.PhaseStart, At: time.Now().UTC(), Kind: runledger.KindWrapper,
		Description: runledger.Truncate(runledger.Clean(description, projectDir), 200),
		Command:     runledger.CleanCommand(strings.Join(shellQuotePaths(argv), " "), projectDir),
		Folder:      runledger.Folder(cwd, projectDir), Commit: runledger.HeadCommit(cwd),
		LogPath: ".wipnote/logs/runs/" + runID + ".log", Session: session, WorkItem: workItem,
		Harness: "wrapper",
	}
}

// execTee runs argv with stdout and stderr copied to the log file and passed
// through. It returns the command's exit code and the run status. Interrupt
// and terminate signals are forwarded to the command, so the end is still
// recorded when the caller presses Ctrl-C.
func execTee(ctx context.Context, argv []string, logPath string, stdout, stderr io.Writer) (int, string) {
	_ = os.MkdirAll(filepath.Dir(logPath), 0o755)
	logFile, err := os.Create(logPath)
	if err != nil {
		logFile = nil
	}
	outW, errW := stdout, stderr
	if logFile != nil {
		defer logFile.Close()
		outW, errW = io.MultiWriter(stdout, logFile), io.MultiWriter(stderr, logFile)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c := exec.CommandContext(ctx, argv[0], argv[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, outW, errW
	if err := c.Start(); err != nil {
		fmt.Fprintf(errW, "error: wipnote run: %v\n", err)
		return 127, runledger.StatusFailed
	}
	stop := forwardSignals(c.Process)
	err = c.Wait()
	stop()
	return exitStatus(err)
}

func forwardSignals(p *os.Process) func() {
	ch := make(chan os.Signal, 2)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case sig := <-ch:
				_ = p.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}

// exitStatus maps a Wait error to the exit code and run status. A command
// killed by a signal exits 128+signal, as a shell reports it.
func exitStatus(err error) (int, string) {
	if err == nil {
		return 0, runledger.StatusCompleted
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return 1, runledger.StatusFailed
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		return 128 + int(ws.Signal()), runledger.StatusKilled
	}
	return exitErr.ExitCode(), runledger.StatusFailed
}
