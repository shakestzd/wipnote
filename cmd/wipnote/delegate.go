package main

import "github.com/spf13/cobra"

// delegateCmd is `wipnote delegate`, the native delegation verbs
// (plan-28b4a7f6). Subcommands register here.
func delegateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Delegate work to worker sessions and record the runs",
	}
	cmd.AddCommand(delegateImportOrchCmd())
	return cmd
}
