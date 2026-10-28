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
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
	"github.com/shakestzd/wipnote/internal/gate"
)

// delegateSpawnCmd is `wipnote delegate spawn NAME --harness claude|codex`
// (plan-28b4a7f6 slice 10): an interactive worker in the caller's workers
// tab. It returns within seconds with status starting.
func delegateSpawnCmd() *cobra.Command {
	var harness, workItem, brief, briefFile, resumeFrom string
	var inContainer bool
	cmd := &cobra.Command{
		Use:   "spawn [NAME] (--harness claude|codex | --resume OLD) [--work-item ID] [--brief TEXT | --brief-file PATH] [--container]",
		Short: "Start an interactive Claude or Codex worker in the caller's herdr workers tab",
		Long: `Starts an interactive worker under wipnote claude or wipnote codex in a split of
the caller's workers tab (one tab per caller, at most 6 workers). spawn types one
short boot line into the new pane and returns {run, name, pane, harness, status}
with status "starting"; the worker's hooks bind its session, and the next
delegate list renames and pins its herdr agent.

The brief goes by file (the run folder's brief.md), never on the typed line, with
a preamble that tells the worker where to write its result. NAME is [a-z0-9-],
1 to 32 characters, starting with a letter, and unique among live workers.
Without NAME the worker is named <item-id>-<slug of its title>, cut to 32; the
name is both its mailbox name and its Claude session name (--name). The
work item defaults to the caller's active claim; spawn refuses without one.

--container starts the worker in the project's own devcontainer: the boot in
the pane runs devcontainer up, then devcontainer exec --workspace-folder <root>
wipnote claude|codex. spawn still returns at once. It refuses without
.devcontainer/devcontainer.json or the devcontainer CLI, and a Codex worker
unless this host is in delegate.codex.dangerFullAccessHosts.

--resume OLD starts NAME on OLD's last harness session (wipnote claude
--resume <id>, wipnote codex --resume <id>) in OLD's recorded folder and place
(host or devcontainer), with the harness and work item of OLD's run unless
given. NAME is required with --resume. The new run's ledger row has
resumed_from set. It refuses while OLD is live, when OLD's run folder was swept,
when no session id was recorded, when another run already continues that
session, and a Codex resume with a brief.`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readBrief(brief, briefFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			name := ""
			if len(args) == 1 {
				name = args[0]
			}
			return runDelegateSpawn(cmd, spawnArgs{name: name, harness: harness, workItem: workItem,
				brief: text, container: inContainer, resume: resumeFrom})
		},
	}
	cmd.Flags().StringVar(&harness, "harness", "", "claude or codex")
	cmd.Flags().StringVar(&workItem, "work-item", "", "work item id (default: the caller's active claim)")
	cmd.Flags().StringVar(&brief, "brief", "", "the worker's first task")
	cmd.Flags().StringVar(&briefFile, "brief-file", "", "read the first task from a file (- for stdin)")
	cmd.Flags().BoolVar(&inContainer, "container", false, "start the worker in the project's own devcontainer")
	cmd.Flags().StringVar(&resumeFrom, "resume", "", "continue the stopped worker OLD's last session, in its folder and place")
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

// spawnArgs is one spawn command's flags.
type spawnArgs struct {
	name, harness, workItem, brief, resume string
	container                              bool
}

func runDelegateSpawn(cmd *cobra.Command, a spawnArgs) error {
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
	if a.workItem == "" && a.resume == "" { // a resume defaults to the old run's item
		a.workItem = gate.ActiveWorkItemForGate(projectDir, caller, db.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID")))
	}
	if a.resume != "" && a.name == "" {
		return fmt.Errorf("delegate spawn: --resume needs NAME for the new worker")
	}
	title := ""
	if a.name == "" {
		a.workItem, title = workItemTitle(wipnoteDir, a.workItem)
	}
	cwd, _ := os.Getwd()
	exe, err := os.Executable()
	if err != nil {
		exe = "wipnote"
	}
	o := session.SpawnOptions{
		Name: a.name, Harness: a.harness, WorkItem: a.workItem, WorkTitle: title, CallerSession: caller, CallerPane: env.CallerPane,
		Brief: a.brief, Cwd: cwd, Container: a.container, Root: projectDir,
		CodexOptIn: (a.container || a.resume != "") && hostOptedIn(projectDir),
		BootLine:   func(run string) string { return session.BootLine(exe, projectDir, run, caller) },
	}
	var s session.Spawned
	if a.resume != "" {
		s, err = session.Resume(cmd.Context(), d, o, a.resume)
	} else {
		s, err = session.Spawn(cmd.Context(), d, o)
	}
	if err != nil {
		return err
	}
	if s.Harness == session.HarnessCodex && spawnedInContainer(d, s.Run) {
		fmt.Fprintln(cmd.ErrOrStderr(), "wipnote: warning: the Codex worker runs with --sandbox danger-full-access inside the devcontainer (bwrap does not work there; this host opted in through delegate.codex.dangerFullAccessHosts)")
	}
	return writeJSON(cmd.OutOrStdout(), s)
}

// spawnedInContainer reports that run's ledger row is a container worker.
func spawnedInContainer(d session.Deps, run string) bool {
	r, ok, err := d.Ledger.Get(run)
	return err == nil && ok && r.Host == session.PlaceContainer
}

// hostOptedIn: this host is in delegate.codex.dangerFullAccessHosts (the
// user config only), which a Codex worker inside the devcontainer needs.
func hostOptedIn(projectDir string) bool {
	s := codexSandboxOptions(projectDir, false)
	return codex.OptedIn(s.Host, s.OptIn)
}

// delegateSessionDeps builds the session verbs' stores, with a herdr client
// only inside herdr.
func delegateSessionDeps(wipnoteDir string) (session.Deps, herdr.Env, error) {
	store, err := runstore.New(filepath.Dir(wipnoteDir))
	if err != nil {
		return session.Deps{}, herdr.Env{}, err
	}
	d := session.Deps{Store: store, Ledger: delegationledger.NewStore(wipnoteDir),
		Claims: claimLedgerStore(wipnoteDir), ExpireClaims: expireDelegateWorkerClaims(wipnoteDir),
		ResolveWorkItem: func(id string) (string, error) { return resolveSpawnWorkItem(wipnoteDir, id) }}
	env := herdr.EnvFrom(os.Getenv)
	if env.InHerdr {
		d.Herdr = herdr.New(env)
	}
	return d, env, nil
}

// resolveSpawnWorkItem turns a work-item id or unambiguous prefix into its
// canonical id, so the one-worker-per-item check compares like with like. An
// unknown or ambiguous id is refused; an empty one is left for Spawn to refuse.
// It is Spawn's Deps.ResolveWorkItem, so it also sees the item a resume
// defaults to the old run's.
func resolveSpawnWorkItem(wipnoteDir, id string) (string, error) {
	if id == "" {
		return "", nil
	}
	full, err := workitem.ResolvePartialID(wipnoteDir, id)
	if err != nil {
		return "", fmt.Errorf("delegate spawn: work item %s: %w", id, err)
	}
	return full, nil
}
