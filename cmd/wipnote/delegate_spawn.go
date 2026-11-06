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
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
	"github.com/shakestzd/wipnote/internal/gate"
)

// delegateSpawnCmd is `wipnote delegate spawn NAME --harness claude|codex`
// (plan-28b4a7f6 slice 10): an interactive worker in the caller's workers
// tab. It returns within seconds with status starting.
func delegateSpawnCmd() *cobra.Command {
	var harness, workItem, brief, briefFile, resumeFrom, model, effort string
	var inContainer, userRules, projectRules, inWorktree bool
	var base string
	var profile, cacheTTL string
	var tools, mcp, plugins []string
	cmd := &cobra.Command{
		Use:   "spawn [NAME] (--harness claude|codex | --resume OLD) [--work-item ID] [--brief TEXT | --brief-file PATH] [--model M] [--effort E] [--profile P] [--tool T] [--mcp S] [--plugin P] [--user-rules] [--cache-ttl 5m|1h] [--container | --worktree [--base REF]] [-- HARNESS_FLAGS]",
		Short: "Start an interactive Claude or Codex worker in the caller's herdr workers tab",
		Long:  delegateSpawnLong,
		Args: func(cmd *cobra.Command, args []string) error {
			return cobra.MaximumNArgs(1)(cmd, spawnNameArgs(cmd, args))
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			text, err := readBrief(brief, briefFile, cmd.InOrStdin())
			if err != nil {
				return err
			}
			name := ""
			names := spawnNameArgs(cmd, args)
			if len(names) == 1 {
				name = names[0]
			}
			return runDelegateSpawn(cmd, spawnArgs{name: name, harness: harness, workItem: workItem,
				brief: text, container: inContainer, resume: resumeFrom, model: model, effort: effort, profile: profile, tools: tools, mcp: mcp, plugins: plugins, userRules: userRules, projectRules: projectRules, cacheTTL: cacheTTL, extra: args[len(names):], worktree: inWorktree, base: base})
		},
	}
	registerSpawnFlags(cmd, &spawnFlagVars{harness: &harness, workItem: &workItem, brief: &brief, briefFile: &briefFile, inContainer: &inContainer, model: &model, profile: &profile, tools: &tools, mcp: &mcp, plugins: &plugins, userRules: &userRules, projectRules: &projectRules, cacheTTL: &cacheTTL, effort: &effort, resumeFrom: &resumeFrom, inWorktree: &inWorktree, base: &base})
	return cmd
}

// delegateSpawnLong is the long help text of delegate spawn.
const delegateSpawnLong = `Starts an interactive worker under wipnote claude or wipnote codex in a split of
the project's workers tab (one tab per project, at most 6 workers). In the Herdr app
the tab shows in the tab bar and each worker is a child of its caller. The wipnote-main
layout (delegate install-layout) lists a worker once, nested under its caller; Herdr's
own main layout also gives each worker a row of its own. Click the tab, or a worker
(terminal.peek), to see them; hc read works too. spawn types one
short boot line into the new pane and returns {run, name, pane, harness, status}
with status "starting"; the worker's hooks bind its session, and the next
delegate list renames and pins its herdr agent.

The brief goes by file (the run folder's brief.md), never on the typed line, with
a preamble that tells the worker where to write its result. NAME is [a-z0-9-],
1 to 32 characters, starting with a letter, and unique among live workers.
Without NAME the worker is named <item-id>-<slug of its title>, cut to 32; the
name is both its mailbox name and its Claude session name (--name). The
work item defaults to the caller's active claim; spawn refuses without one.

--worktree gives the worker its own git worktree at
<root>/.claude/worktrees/<name> on a new branch delegate/<name>, cut from --base
(default HEAD), and runs it there; hooks and the ledger stay on the project root.
Because each worker has its own checkout, several workers may share one work item
(without --worktree there is one live worker per item). The cap of 6 still holds.
Use it when workers edit overlapping files; each branch is merged on its own.
A taken name or branch refuses the spawn. It is for Claude workers and refuses
--container and --resume. delegate stop removes the worktree and branch when they
have no changes, no untracked files and no commits; otherwise it keeps them and
reports {worktree, branch, kept}. A resume of a stopped worktree worker runs in its
worktree while that folder still exists. A worker spawned from a linked worktree
runs in that worktree.

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
session, and a Codex resume with a brief.

--model picks the worker's model, so small work can go to a cheaper one, and
--effort its reasoning effort. Other harness flags go after --, such as
--mcp-config m.json --strict-mcp-config --allowedTools mcp__x__y. A flag that
ends the interactive session or sets its identity (-p, --resume, --session-id,
--name) is refused. A resume with none of these keeps the old run's. The model
is recorded on the run's ledger row.

A Claude worker starts lean, and YOU choose its setup per task:
--profile simple (haiku, low effort: small, well-defined work), standard (sonnet,
medium; the default), deep (opus, high) or full (Claude's own start: your model
and effort, ~/.claude/CLAUDE.md, MCP, skills, orchestrator prompt). Lean drops
~/.claude/CLAUDE.md, MCP servers, auto memory, skills, subagents, Chrome, git
status in the prompt and the orchestrator prompt, and gives only Bash, Read, Edit,
Write, Grep and Glob: about a fifth of the usual first-request prompt. --model and --effort
override the profile. Add only what the task needs: --tool T (WebFetch,
Agent...), --mcp NAME (a local server, a "claude.ai Notion" connector or a
plugin:x:y server; ToolSearch is added), --plugin NAME (Skill is added),
--user-rules (keep ~/.claude/CLAUDE.md; otherwise put the rules in the brief),
--project-rules (keep the project CLAUDE.md and rules, which simple and standard
leave out: the worker is told, and can read them if the task needs them).
The prompt cache lives 5m by default, which suits short workers; use --cache-ttl 1h
for a worker that will sit idle between tasks.
An unknown profile, server or plugin refuses the spawn and lists the valid ones.
Your own profiles go in ~/.wipnote/config.json under delegate.claude.profiles
(model, effort, tools, mcp, plugins, userRules, projectRules, cacheTTL), and delegate.claude.defaultProfile
picks the default. A --tools, --chrome, --settings or --strict-mcp-config after
-- replaces its default (your --settings replaces the whole overlay). A resume
that gives none of these keeps the old run's setup. The result shows profile,
model, effort, tools, mcp, plugins and cacheTTL. Codex workers (which refuse these flags)
and delegate run are unchanged.`

// spawnFlagVars points at the variables the spawn flags fill.
type spawnFlagVars struct {
	harness, workItem, brief, briefFile, model, profile, cacheTTL, effort, resumeFrom, base *string
	inContainer, userRules, projectRules, inWorktree                                        *bool
	tools, mcp, plugins                                                                     *[]string
}

// registerSpawnFlags registers the delegate spawn flags on cmd.
func registerSpawnFlags(cmd *cobra.Command, v *spawnFlagVars) {
	f := cmd.Flags()
	f.StringVar(v.harness, "harness", "", "claude or codex")
	f.StringVar(v.workItem, "work-item", "", "work item id (default: the caller's active claim)")
	f.StringVar(v.brief, "brief", "", "the worker's first task")
	f.StringVar(v.briefFile, "brief-file", "", "read the first task from a file (- for stdin)")
	f.BoolVar(v.inContainer, "container", false, "start the worker in the project's own devcontainer")
	f.StringVar(v.model, "model", "", "the worker's model, such as haiku")
	f.StringVar(v.profile, "profile", "", "Claude worker profile: simple, standard (default), deep, full, or one from delegate.claude.profiles")
	f.StringArrayVar(v.tools, "tool", nil, "add a built-in Claude tool to Bash,Read,Edit,Write,Grep,Glob (repeatable), such as WebFetch")
	f.StringArrayVar(v.mcp, "mcp", nil, "turn on exactly this MCP server (repeatable); claude.ai connectors and plugin servers by their `claude mcp list` name")
	f.StringArrayVar(v.plugins, "plugin", nil, "turn on exactly this Claude plugin (repeatable), name or name@marketplace")
	f.BoolVar(v.userRules, "user-rules", false, "keep ~/.claude/CLAUDE.md for the worker")
	f.BoolVar(v.projectRules, "project-rules", false, "keep the project CLAUDE.md and rules (off in simple and standard)")
	f.StringVar(v.cacheTTL, "cache-ttl", "", "prompt cache lifetime, 5m (default) or 1h; 1h for a worker that sits idle between tasks")
	f.StringVar(v.effort, "effort", "", "the worker's reasoning effort, such as low")
	f.BoolVar(v.inWorktree, "worktree", false, "give the worker its own git worktree and branch delegate/NAME, so workers can share a work item")
	f.StringVar(v.base, "base", "", "with --worktree: the ref the branch is cut from (default HEAD)")
	f.StringVar(v.resumeFrom, "resume", "", "continue the stopped worker OLD's last session, in its folder and place")
}

// spawnNameArgs is the positional words before --; those after it are harness
// flags.
func spawnNameArgs(cmd *cobra.Command, args []string) []string {
	if dash := cmd.ArgsLenAtDash(); dash >= 0 {
		return args[:dash]
	}
	return args
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
	model, effort                          string
	profile                                string
	tools, mcp, plugins                    []string
	userRules                              bool
	projectRules                           bool
	cacheTTL                               string
	extra                                  []string
	container                              bool
	worktree                               bool
	base                                   string
}

func runDelegateSpawn(cmd *cobra.Command, a spawnArgs) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	// projectDir is the store owner: the run store, the ledgers and the
	// workers' --project-dir. Worktrees go in the source repo, which the session
	// package gets from paths.CodeRoot(Root).
	projectDir := filepath.Dir(wipnoteDir)
	if a.container && paths.IsExternalStore(projectDir) {
		return errContainerExternalStore
	}
	d, env, err := delegateSessionDeps(wipnoteDir)
	if err != nil {
		return err
	}
	d.PreseedTrust = oneshot.PreseedTrust
	caller := hooks.EnvSessionID("")
	if a.workItem == "" && a.resume == "" { // a resume defaults to the old run's item
		a.workItem = gate.ActiveWorkItemForGate(projectDir, caller, db.NormaliseAgentID(os.Getenv("WIPNOTE_AGENT_ID")))
	}
	if a.resume != "" && a.name == "" {
		return fmt.Errorf("delegate spawn: --resume needs NAME for the new worker")
	}
	// The title also goes into the terminal name, so a named worker needs it too.
	title := ""
	if a.name == "" {
		a.workItem, title = workItemTitle(wipnoteDir, a.workItem)
	} else if a.workItem != "" {
		_, title = workItemTitle(wipnoteDir, a.workItem)
	}
	if a.base != "" && !a.worktree {
		return fmt.Errorf("delegate spawn: --base needs --worktree")
	}
	cwd, _ := os.Getwd()
	exe, err := os.Executable()
	if err != nil {
		exe = "wipnote"
	}
	o := session.SpawnOptions{
		Name: a.name, Harness: a.harness, WorkItem: a.workItem, WorkTitle: title, CallerSession: caller, CallerPane: env.CallerPane,
		Brief: a.brief, Cwd: cwd, Container: a.container, Root: projectDir, Model: a.model, Effort: a.effort, Profile: a.profile, Tools: a.tools, MCP: a.mcp, Plugins: a.plugins, UserRules: a.userRules, ProjectRules: a.projectRules, CacheTTL: a.cacheTTL,
		Config: session.LoadClaudeConfig(paths.UserConfigPath()), Extra: a.extra, Worktree: a.worktree, Base: a.base,
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
		d.Herdr = herdr.NewClient(env, 0)
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
