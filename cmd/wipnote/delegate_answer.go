package main

import (
	"errors"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

func delegateAnswerCmd() *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "answer NAME QUESTION-ID [TEXT | --file PATH|-]",
		Short: "Answer a worker's pending question and clear it",
		Long: `Records the answer to one of the worker's pending questions (an id from
delegate questions) as an inbox note in reply to it, so the question drops out
of questions, list and wait; then types the text into the worker's pane as a
hint. The inbox is authoritative: a hint that fails leaves the answer there.
Refused, writing nothing, for an id that is not pending. Prints
{run, name, question, answer_id, hinted, hint_error}.`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			text := ""
			if len(args) == 3 {
				text = args[2]
			}
			text, err := readBrief(text, file, cmd.InOrStdin())
			if err != nil {
				return err
			}
			if text == "" {
				return errors.New("delegate answer: no answer text (pass TEXT or --file)")
			}
			return delegateSessionVerb(func(d session.Deps) error {
				out, err := session.Answer(cmd.Context(), d, args[0], args[1], text)
				if err != nil {
					return err
				}
				return writeJSON(cmd.OutOrStdout(), out)
			})
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "read the answer from a file (- for stdin)")
	return cmd
}
