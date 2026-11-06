package main

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

func delegateWatchCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "watch [NAME] [--timeout D]",
		Short: "Stream worker state changes, one line each (for Claude Code's Monitor tool)",
		Long: `With no NAME, one watch covers every live worker of the project, and workers
spawned later join it; each line names its worker, and the watch ends when no
worker is left to watch or at the timeout. With a NAME it watches that worker.
Meant for Claude Code's Monitor tool, or "| while read". Prints one key=value
line each time the worker's state changes, flushed at once, and nothing while
it holds: run=R name=N state=working | asks | blocked | done | ...
A pending question prints as state=asks asks=N q="<first question, bounded>".

Unlike wait, asks does not end the watch: answer it with delegate send and
keep watching. The watch ends on done, idle_no_result, idle_shell, ready,
blocked, gone, stopped or the timeout. --timeout defaults to 25 minutes, under
Monitor's 30-minute cap; on a timeout the last line says to run watch again.
Polls at most twice a second and writes no run state. Exit status is wait's:
124 for a timeout, 3 for gone, 4 for blocked, 0 for every other outcome, 1 for
an error.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			code, err := runDelegateWatch(cmd, name, timeout)
			if err == nil && code != 0 {
				delegateWaitExit(code)
			}
			return err
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", session.DefaultWatchTimeout, "how long to watch")
	return cmd
}

// runDelegateWatch streams the lines to the command's stdout and returns the
// exit status.
func runDelegateWatch(cmd *cobra.Command, name string, timeout time.Duration) (int, error) {
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	out := bufio.NewWriter(cmd.OutOrStdout())
	emit := func(line string) {
		_, _ = io.WriteString(out, line+"\n")
		_ = out.Flush()
	}
	code := 0
	err := delegateSessionVerb(func(d session.Deps) error {
		opts := session.WaitOptions{Timeout: timeout}
		var res session.WaitResult
		var err error
		if name == "" {
			res, err = session.WatchAll(ctx, d, opts, emit)
		} else {
			res, err = session.Watch(ctx, d, name, opts, emit)
		}
		code = res.ExitCode()
		return err
	})
	return code, err
}
