package main

import "github.com/spf13/cobra"

// delegateCmd is `wipnote delegate`, native delegation (plan-28b4a7f6). Its
// verbs are added by the slices that build them; view (slice 7) is first.
func delegateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Delegated Codex and Claude runs: follow a run's log and answer",
	}
	cmd.AddCommand(delegateViewCmd())
	return cmd
}
