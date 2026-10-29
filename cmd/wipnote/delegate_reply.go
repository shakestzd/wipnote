package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateReplyCmd is the worker side of a task: it writes the answer
// through the run store, so the worker never writes into .wipnote/ itself,
// which its hooks forbid (bug-66574a82).
func delegateReplyCmd() *cobra.Command {
	var o session.ReplyOptions
	var file string
	cmd := &cobra.Command{
		Use:   "reply [TEXT | --file PATH|-]",
		Short: "In a worker: answer your task, or ask your caller a question",
		Long: `Runs inside a delegate worker, for the run named by WIPNOTE_DELEGATE_RUN.
Writes the answer as the task's result file (outbox/<task>-result.json), which
delegate wait and result read; with --ask, writes the task's next question file
(outbox/<task>-q<n>.json) instead. --task defaults to the run's newest task.
Prints {run, name, task, id, kind}.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			run := os.Getenv(session.RunEnv)
			if run == "" {
				return fmt.Errorf("delegate reply: %s is not set; reply runs inside a delegate worker", session.RunEnv)
			}
			text := ""
			if len(args) == 1 {
				text = args[0]
			}
			if text != "" && file != "" {
				return fmt.Errorf("delegate reply: pass TEXT or --file, not both")
			}
			text, err := readBrief(text, file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			o.Body = text
			return delegateSessionVerb(func(d session.Deps) error {
				out, err := session.Reply(d, run, o)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), out)
			})
		},
	}
	cmd.Flags().StringVar(&o.Task, "task", "", "the task id to answer (default: the run's newest task)")
	cmd.Flags().BoolVar(&o.Ask, "ask", false, "write a question for your caller instead of the result")
	cmd.Flags().StringArrayVar(&o.Options, "option", nil, "a choice for --ask (repeat for each)")
	cmd.Flags().StringVar(&file, "file", "", "read the reply from a file (- for stdin)")
	return cmd
}
