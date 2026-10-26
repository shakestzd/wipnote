package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// ciWatchInterrupted is the exit code after Ctrl-C (128 + SIGINT).
const ciWatchInterrupted = 130

// ciWatchOpts are the ci watch flags.
type ciWatchOpts struct {
	jsonLines      bool
	timeout        time.Duration
	checksInterval time.Duration
	slowInterval   time.Duration
}

func ciWatchCmd() *cobra.Command {
	var o ciWatchOpts
	cmd := &cobra.Command{
		Use:   "watch [pr|branch]",
		Short: "Follow a pull request's CI in the foreground until it settles",
		Long: "Poll a pull request (resolved as in 'ci status') until no check is pending, the PR\n" +
			"is merged or closed, or --timeout passes. Checks and PR state are read every\n" +
			"--interval-checks while any check is pending; review, base, release and branch\n" +
			"health every --interval-slow, and everything then when nothing is pending. Output\n" +
			"repeats on every change only. With no PR it prints one snapshot and exits 0.\n" +
			"A foreground loop: no daemon, never run from a hook.\n\n" +
			"Exit codes: 0 all passed, 1 a check failed, 2 timeout, 3 not available, 130 interrupted.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			code, err := runCIWatch(ctx, os.Stdout, gitwatch.ExecRunner{}, gitwatch.RealClock{}, arg, o)
			stop()
			if err == nil && code != 0 {
				os.Exit(code)
			}
			return err
		},
	}
	f := cmd.Flags()
	f.BoolVar(&o.jsonLines, "json-lines", false, "Print one compact JSON snapshot per line, on each change")
	f.DurationVar(&o.timeout, "timeout", gitwatch.DefaultWatchTimeout, "Stop watching after this long (exit 2)")
	f.DurationVar(&o.checksInterval, "interval-checks", gitwatch.DefaultChecksInterval, "Checks poll interval while any is pending (min 5s)")
	f.DurationVar(&o.slowInterval, "interval-slow", gitwatch.DefaultSlowInterval, "Review, base, release and branch poll interval (min 5s)")
	return cmd
}

// runCIWatch watches arg and writes each changed snapshot to w. It returns
// the exit code; Ctrl-C is ciWatchInterrupted, not an error.
func runCIWatch(ctx context.Context, w io.Writer, r gitwatch.Runner, clock gitwatch.Clock, arg string, o ciWatchOpts) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	code, err := gitwatch.Watch(ctx, r, gitwatch.WatchOptions{
		Arg: arg, Timeout: o.timeout, ChecksInterval: o.checksInterval, SlowInterval: o.slowInterval,
		Clock: clock, Emit: watchEmitter(w, o.jsonLines),
	})
	if errors.Is(err, context.Canceled) {
		return ciWatchInterrupted, nil
	}
	return code, err
}

// watchEmitter prints a snapshot as one JSON line, or as a timestamp line
// followed by the ci status text summary.
func watchEmitter(w io.Writer, jsonLines bool) func(*gitwatch.Snapshot) error {
	return func(s *gitwatch.Snapshot) error {
		if jsonLines {
			data, err := json.Marshal(s)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(w, "%s\n", data)
			return err
		}
		fmt.Fprintf(w, "--- %s\n", s.FetchedAt.Format(time.RFC3339))
		writeSnapshotText(w, s)
		return nil
	}
}
