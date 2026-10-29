package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// The mailbox verbs (plan-28b4a7f6 slice 11): send, wait, result and
// questions run in the caller; inbox runs in the worker.

// delegateSessionVerb runs fn with the session verbs' deps.
func delegateSessionVerb(fn func(session.Deps) error) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	d, _, err := delegateSessionDeps(wipnoteDir)
	if err != nil {
		return err
	}
	return fn(d)
}

func delegateSendCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "send NAME [TEXT | --file PATH|-]",
		Short: "Give a live worker a task: an inbox file, then a herdr hint",
		Long: `Writes the task to the worker's inbox first, with a preamble naming its result
file (outbox/<task>-result.json) and question files (outbox/<task>-q<n>.json),
then types it with herdr agent prompt as a hint. The inbox is authoritative: a
hint that fails leaves the task there, and the worker's delegate inbox shows it.
Refused, writing nothing, unless the worker accepts input and its input line
holds no draft (a grey next-prompt suggestion reads as empty). Prints
{run, name, task, turn, hinted, hint_error}.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := ""
			if len(args) == 2 {
				text = args[1]
			}
			text, err := readBrief(text, file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if text == "" {
				return errors.New("delegate send: no task text (pass TEXT or --file)")
			}
			return delegateSessionVerb(func(d session.Deps) error {
				sent, err := session.Send(cmd.Context(), d, args[0], text)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), sent)
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the task from a file (- for stdin)")
	return cmd
}

// delegateWaitExit ends the process with wait's non-zero status; tests
// replace it.
var delegateWaitExit = os.Exit

func delegateWaitCmd() *cobra.Command {
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "wait NAME --timeout D",
		Short: "Block until a worker's durable signal or the timeout",
		Long: `Settles on durable signals only: the current task's result file (even with
silent hooks or a turn stuck at working), a stop (at once), a lost pane, a
pending question (hook or outbox q-file), or the Stop hook's turn-ended signal
for the current task (an older turn's never counts). herdr state and the input
line are never read as done. Polls at most twice a second and writes no run
state. Prints {run, name, turn, outcome, task, detail, asks}: outcome is ready,
done, asks, idle_no_result, idle_shell, blocked, timeout, gone or stopped.
--timeout is required. Exit status: 124 for a timeout (which changes no
claim), 3 for gone (the worker is lost), 4 for blocked (the worker waits at a
dialog in its pane), 0 for every other outcome, 1 for an error.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !cmd.Flags().Changed("timeout") {
				return session.ErrNoTimeout
			}
			code, err := runDelegateWait(cmd, args[0], timeout)
			if err == nil && code != 0 {
				delegateWaitExit(code)
			}
			return err
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "how long to wait (required)")
	return cmd
}

// runDelegateWait waits, prints the payload and returns the exit status.
func runDelegateWait(cmd *cobra.Command, name string, timeout time.Duration) (int, error) {
	parent := cmd.Context()
	if parent == nil {
		parent = context.Background()
	}
	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()
	code := 0
	err := delegateSessionVerb(func(d session.Deps) error {
		res, err := session.Wait(ctx, d, name, session.WaitOptions{Timeout: timeout})
		if err != nil {
			return err
		}
		code = res.ExitCode()
		return writeJSON(cmd.OutOrStdout(), res)
	})
	return code, err
}

func delegateResultCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "result NAME",
		Short: "Print the newest result file a worker wrote",
		Long: `Prints the newest outbox result file's body and the task it answers, whatever
the worker's state, or {"result": "none"}. Prints {run, name, result, task,
body, created}.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return delegateSessionVerb(func(d session.Deps) error {
				r, err := session.Result(d, args[0])
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), r)
			})
		},
	}
}

func delegateQuestionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "questions NAME",
		Short: "List a worker's pending questions",
		Long: `Prints the pending AskUserQuestion and PermissionRequest calls the worker's
hooks recorded, and its current task's unanswered outbox question files
(<task>-q<n>.json, listed even when the hooks are silent), as JSON rows
{id, kind, task, tool, text, options}. Answered ones are dropped.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return delegateSessionVerb(func(d session.Deps) error {
				qs, err := session.Questions(d, args[0])
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), qs)
			})
		},
	}
}

func delegateInboxCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inbox",
		Short: "In a worker: read your own mailbox",
		Long: `Runs inside a delegate worker. Reads the inbox of the run named by
WIPNOTE_DELEGATE_RUN, including tasks whose herdr hint was lost (lost: true),
as JSON rows {id, seq, type, from, turn, created, hinted, acked, lost, body}.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			run := os.Getenv(session.RunEnv)
			if run == "" {
				return fmt.Errorf("delegate inbox: %s is not set; inbox runs inside a delegate worker", session.RunEnv)
			}
			return delegateSessionVerb(func(d session.Deps) error {
				rows, err := session.Inbox(d, run)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), rows)
			})
		},
	}
}
