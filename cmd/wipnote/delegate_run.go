package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/gate"
)

// delegateRunExit ends the process with the tool's exit status; tests
// replace it.
var delegateRunExit = os.Exit

// delegateRunCmd is `wipnote delegate run [--label] [--work-item] [--no-pane]
// -- <cmd>` (plan-28b4a7f6 slice 8): one codex exec or review, claude -p or
// ccx task, in a herdr pane inside herdr, else headless, with the
// codex-herdr-pane mod's report. The exit status is the tool's.
func delegateRunCmd() *cobra.Command {
	var label, workItem string
	var noPane, write bool
	cmd := &cobra.Command{
		Use:   "run [--label L] [--work-item ID] [--no-pane] [--write] -- <codex exec|codex review|claude -p|ccx task> ...",
		Short: "Run one delegated codex, claude -p or ccx call and print its report",
		Long: `Runs one tool call (codex exec or review, claude -p, ccx task) as a recorded
delegation: a row in .wipnote/delegations-ledger.html, a run folder under
.wipnote/delegate/runs/, and a herdr pane when HERDR_ENV=1 (headless otherwise,
or with --no-pane). Prints a status line, the answer (or the end of the log on
failure) and the run page. Exits with the tool's status, 130 if its pane closed.

Only --label enters the ledger; a derived pane title stays in the run folder.
Without --label the pane title is "<id>: <title>" of the work item, or one
derived from the prompt when there is none. The work item defaults to the caller's active claim. Chained commands (pipes,
;) are not supported. Settings: the "delegate" object (layout, maxPanes,
closePane, closeDelay, tailLines, maxReturnLines, leanProfile, leanProfileName,
ccxRunsDir, ccxWaitSeconds, keepDays), merged per key from ~/.wipnote/config.json,
then .wipnote/config.json, then .wipnote/config.local.json (gitignored). The
host path ccxRunsDir is read only from the user and local files. CCX_RUNS_DIR
overrides ccxRunsDir.

codex exec runs with --sandbox read-only, or workspace-write with --write.
danger-full-access is passed, with a warning, only on a host listed in
delegate.codex.dangerFullAccessHosts in ~/.wipnote/config.json. When Codex's
bwrap sandbox fails on this host (a cached probe; see wipnote delegate
doctor) the run is refused with the probe output unless the host opted in.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runDelegateRun(cmd, args, label, workItem, runFlags{noPane: noPane, write: write})
			if err != nil {
				return err
			}
			if code != 0 {
				delegateRunExit(code)
			}
			return nil
		},
	}
	cmd.Flags().SetInterspersed(false) // the tool's own flags are never wipnote's
	cmd.Flags().StringVar(&label, "label", "", "pane title; the only label the ledger keeps")
	cmd.Flags().StringVar(&workItem, "work-item", "", "work item id (default: the caller's active claim)")
	cmd.Flags().BoolVar(&noPane, "no-pane", false, "run headless even inside herdr")
	cmd.Flags().BoolVar(&write, "write", false, "codex exec: --sandbox workspace-write instead of read-only")
	return cmd
}

// runFlags are delegate run's boolean flags.
type runFlags struct{ noPane, write bool }

func runDelegateRun(cmd *cobra.Command, argv []string, label, workItem string, f runFlags) (int, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return 0, err
	}
	projectDir := filepath.Dir(wipnoteDir)
	store, err := runstore.New(projectDir)
	if err != nil {
		return 0, err
	}
	session := hooks.EnvSessionID("")
	if workItem == "" {
		workItem = gate.ActiveWorkItemForGate(projectDir, session, db.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID")))
	}
	_, workTitle := workItemTitle(wipnoteDir, workItem)
	cwd, _ := os.Getwd()
	env := herdr.EnvFrom(os.Getenv)
	sandbox := codexSandboxOptions(projectDir, f.write)
	for _, n := range sandbox.notes {
		fmt.Fprintln(cmd.ErrOrStderr(), n)
	}
	o := oneshot.Options{
		Argv: argv, Label: label, WorkItem: workItem, WorkTitle: workTitle, CallerSession: session, NoPane: f.noPane, Cwd: cwd,
		Sandbox:  sandbox.SandboxOptions,
		Settings: oneshot.LoadSettings(projectDir, cmd.ErrOrStderr()), Store: store, Ledger: delegationledger.NewStore(wipnoteDir),
		HerdrEnv: env, PaneLine: delegatePaneLine(projectDir), Stdout: cmd.OutOrStdout(), Stderr: cmd.ErrOrStderr(),
	}
	if env.InHerdr {
		o.Herdr = herdr.New(env)
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	res, err := oneshot.Run(ctx, o)
	return res.Exit, err
}

// delegatePaneLine runs this binary's hidden run-pane verb in the pane.
func delegatePaneLine(projectDir string) func(string) string {
	exe, err := os.Executable()
	if err != nil {
		exe = "wipnote"
	}
	return func(runID string) string {
		return oneshot.PaneLine(exe, []string{"delegate", "run-pane"}, projectDir, runID)
	}
}

// delegateRunPaneCmd is the pane worker a run's herdr pane executes: it runs
// the run folder's job.json while the Go viewer follows the log, closes the
// run's ledger row when it is still open, then closes the pane per
// closePane. Internal; launched only by delegate run.
func delegateRunPaneCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "run-pane <run-id>",
		Short:  "Run a delegate run's job inside its herdr pane (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			store, err := runstore.New(filepath.Dir(wipnoteDir))
			if err != nil {
				return err
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			code := oneshot.RunPane(ctx, oneshot.PaneOptions{
				Store: store, RunID: args[0], Herdr: herdr.New(herdr.EnvFrom(os.Getenv)), Out: cmd.OutOrStdout(),
				Ledger: delegationledger.NewStore(wipnoteDir),
			})
			if code != 0 {
				delegateRunExit(code)
			}
			return nil
		},
	}
}
