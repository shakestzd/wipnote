package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
	"github.com/shakestzd/wipnote/internal/gate"
)

// delegateSpawnCmd is `wipnote delegate spawn NAME --harness claude|codex`
// (plan-28b4a7f6 slice 10): an interactive worker in the caller's workers
// tab. It returns within seconds with status starting.
func delegateSpawnCmd() *cobra.Command {
	var harness, workItem, brief, briefFile string
	cmd := &cobra.Command{
		Use:   "spawn NAME --harness claude|codex [--work-item ID] [--brief TEXT | --brief-file PATH]",
		Short: "Start an interactive Claude or Codex worker in the caller's herdr workers tab",
		Long: `Starts an interactive worker under wipnote claude or wipnote codex in a split of
the caller's workers tab (one tab per caller, at most 6 workers). spawn types one
short boot line into the new pane and returns {run, name, pane, harness, status}
with status "starting"; the worker's hooks bind its session, and the next
delegate list renames and pins its herdr agent.

The brief goes by file (the run folder's brief.md), never on the typed line, with
a preamble that tells the worker where to write its result. NAME is [a-z0-9-],
1 to 32 characters, starting with a letter, and unique among live workers. The
work item defaults to the caller's active claim; spawn refuses without one.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readBrief(brief, briefFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			return runDelegateSpawn(cmd, args[0], harness, workItem, text)
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "claude or codex")
	cmd.Flags().StringVar(&workItem, "work-item", "", "work item id (default: the caller's active claim)")
	cmd.Flags().StringVar(&brief, "brief", "", "the worker's first task")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the first task from a file (- for stdin)")
	return cmd
}

func readBrief(text, file string, stdin io.Reader) (string, error) {
	switch {
	case text != "" && file != "":
		return "", fmt.Errorf("delegate spawn: pass --brief or --brief-file, not both")
	case file == "-":
		data, err := io.ReadAll(stdin)
		return string(data), err
	case file != "":
		data, err := os.ReadFile(file)
		return string(data), err
	}
	return text, nil
}

func runDelegateSpawn(cmd *cobra.Command, name, harness, workItem, brief string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	projectDir := filepath.Dir(wipnoteDir)
	d, env, err := delegateSessionDeps(wipnoteDir)
	if err != nil {
		return err
	}
	caller := hooks.EnvSessionID("")
	if workItem == "" {
		workItem = gate.ActiveWorkItemForGate(projectDir, caller, db.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID")))
	}
	cwd, _ := os.Getwd()
	exe, err := os.Executable()
	if err != nil {
		exe = "wipnote"
	}
	s, err := session.Spawn(cmd.Context(), d, session.SpawnOptions{
		Name: name, Harness: harness, WorkItem: workItem, CallerSession: caller, CallerPane: env.CallerPane,
		Brief: brief, Cwd: cwd,
		BootLine: func(run string) string { return session.BootLine(exe, projectDir, run, caller) },
	})
	if err != nil {
		return err
	}
	return writeJSON(cmd.OutOrStdout(), s)
}

// delegateSessionDeps builds the session verbs' stores, with a herdr client
// only inside herdr.
func delegateSessionDeps(wipnoteDir string) (session.Deps, herdr.Env, error) {
	store, err := runstore.New(filepath.Dir(wipnoteDir))
	if err != nil {
		return session.Deps{}, herdr.Env{}, err
	}
	d := session.Deps{Store: store, Ledger: delegationledger.NewStore(wipnoteDir),
		ExpireClaims: expireDelegateWorkerClaims(wipnoteDir)}
	env := herdr.EnvFrom(os.Getenv)
	if env.InHerdr {
		d.Herdr = herdr.New(env)
	}
	return d, env, nil
}
