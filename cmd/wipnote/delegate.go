package main

import "github.com/spf13/cobra"

// delegateLong is the steps the retired delegate guide held
// (feat-8f910039): agents ran delegate often and rarely opened the skill, so
// the steps live where they already look. Keep the help under 40 lines.
const delegateLong = `Hands work to another harness; each run is a row in the delegations ledger.
One call: wipnote delegate run [--label L] -- <codex exec|claude -p|ccx task ...>
A worker, in herdr or an hc terminal, on a work item you hold:
  1. wipnote delegate spawn NAME --harness claude|codex --brief-file F [--profile P]
  2. wipnote delegate send NAME TASK, then wipnote delegate wait NAME --timeout 20m
  3. wipnote delegate result NAME, then wipnote delegate stop NAME before you complete the item
Profiles: wipnote delegate profiles. Put rules the worker must follow in its brief.
Never call codex exec, claude -p, ccx task or hc run yourself: they skip the ledger.`

// delegateCmd is `wipnote delegate`, the native delegation verbs
// (plan-28b4a7f6). Each slice that builds a verb registers it here.
func delegateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delegate",
		Short: "Delegate work to Codex and Claude worker sessions and record the runs",
		Long:  delegateLong,
	}
	cmd.AddCommand(delegateImportOrchCmd())
	cmd.AddCommand(delegateViewCmd())
	cmd.AddCommand(delegateRunCmd())
	cmd.AddCommand(delegateInstallLayoutCmd())
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
	cmd.AddCommand(delegateAnswerCmd())
	cmd.AddCommand(delegateInboxCmd())
	cmd.AddCommand(delegateReplyCmd())
	cmd.AddCommand(delegateDoctorCmd())
	cmd.AddCommand(delegateProfilesCmd())
	return cmd
}
