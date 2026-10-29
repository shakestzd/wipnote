package main

import "github.com/spf13/cobra"

// delegateCmd is `wipnote delegate`, the native delegation verbs
// (plan-28b4a7f6). Each slice that builds a verb registers it here.
func delegateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Delegate work to Codex and Claude worker sessions and record the runs",
	}
	cmd.AddCommand(delegateImportOrchCmd())
	cmd.AddCommand(delegateViewCmd())
	cmd.AddCommand(delegateRunCmd())
	cmd.AddCommand(delegateRunPaneCmd())
	cmd.AddCommand(delegateSpawnCmd())
	cmd.AddCommand(delegateBootCmd())
	cmd.AddCommand(delegateStatusLineCmd())
	cmd.AddCommand(delegateStopCmd())
	cmd.AddCommand(delegateListCmd())
	cmd.AddCommand(delegateTreeCmd())
	cmd.AddCommand(delegateSendCmd())
	cmd.AddCommand(delegateWaitCmd())
	cmd.AddCommand(delegateWatchCmd())
	cmd.AddCommand(delegateResultCmd())
	cmd.AddCommand(delegateQuestionsCmd())
	cmd.AddCommand(delegateInboxCmd())
	cmd.AddCommand(delegateReplyCmd())
	cmd.AddCommand(delegateDoctorCmd())
	return cmd
}
