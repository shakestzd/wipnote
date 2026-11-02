package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Spawn waits.
const (
	DefaultShellWait = 2 * time.Second
	DefaultPoll      = 100 * time.Millisecond
)

// SpawnOptions is one spawn. WorkItem is required: the caller resolves the
// default (the active claim) before calling. An empty Name is derived from
// the work item and WorkTitle: <item-id>-<slug>, cut to 32 (plan-28b4a7f6
// slice 15). The name is the mailbox name and the harness name alike:
// BootArgv passes it to Claude as --name and list renames the herdr agent.
type SpawnOptions struct {
	Name, Harness, WorkItem string
	WorkTitle               string
	CallerSession           string
	CallerPane              herdr.PaneID
	Brief                   string
	Cwd                     string
	// BootLine builds the line typed into the pane for a run id.
	BootLine func(runID string) string
	// Container starts the worker in the project's devcontainer (slice 21);
	// Root is the project root. CodexOptIn: this host is in
	// delegate.codex.dangerFullAccessHosts (Codex needs it inside).
	Container  bool
	Root       string
	CodexOptIn bool
	// ResumedFrom and ResumeSession are a resume's old run id and the
	// harness session it continues (slice 20); Resume sets both.
	ResumedFrom, ResumeSession string
	// LaunchArgs, when set, gives the harness flags boot adds after the
	// brief, once the run folder exists (it may write files there); a
	// Claude one-shot run's wrapper (slice 22). SessionID and Transcript are
	// recorded with them in session.json.
	LaunchArgs                     func(rf *runstore.RunFolder) ([]string, error)
	SessionID, Transcript, WorkDir string
	// Model and Effort are the harness's --model and effort; Extra are other
	// harness flags (--mcp-config, --allowedTools). Spawn turns them into Flags,
	// which boot passes and a resume inherits (see harnessFlags).
	Model, Effort string
	Extra, Flags  []string
	// Full turns the lean Claude defaults off (see LeanFlags). Lean is what
	// harnessFlags decided, which a resume inherits; inherited marks a resume
	// that chose nothing and so keeps the old run's Flags as they are.
	Full, Lean, inherited bool
	// Profile names the worker's profile ("" is the default); Tools, MCP and
	// Plugins add built-in tools, MCP servers and plugins to it; UserRules
	// keeps ~/.claude/CLAUDE.md. Claude only (see applyProfile, LeanFlags).
	Profile             string
	Tools, MCP, Plugins []string
	UserRules           bool
	CacheTTL            string       // "5m" (default) or "1h"; --cache-ttl beats the profile
	Config              ClaudeConfig // delegate.claude from the user's config
	mcpConfig           []byte       // the generated --mcp-config, written to the run folder

	// Worktree gives the worker its own checkout on a new branch, cut from
	// Base (HEAD when empty); wt is that checkout, or a resumed worker's.
	Worktree bool
	Base     string
	wt       wtInfo

	ProjectDir string        // with WorkDir: the run store's project (Info.ProjectDir)
	ShellWait  time.Duration // DefaultShellWait when zero
	Poll       time.Duration // DefaultPoll when zero
}

// Spawned is spawn's payload: {run, name, pane, harness, status}.
type Spawned struct {
	Run     string `json:"run"`
	Name    string `json:"name"`
	Pane    string `json:"pane"`
	Harness string `json:"harness"`
	Status  string `json:"status"`
	Model   string `json:"model,omitempty"`
	// Setup is what the worker was given (Claude): profile, effort, tools,
	// MCP servers and plugins, so the delegator sees it.
	Profile string   `json:"profile,omitempty"`
	Effort  string   `json:"effort,omitempty"`
	Tools   []string `json:"tools,omitempty"`
	MCP     []string `json:"mcp,omitempty"`
	Plugins []string `json:"plugins,omitempty"`
	// Worktree and Branch are a --worktree worker's own checkout and branch.
	Worktree string `json:"worktree,omitempty"`
	Branch   string `json:"branch,omitempty"`
	Warning  string `json:"warning,omitempty"`
}

// Spawn starts a worker and returns with status starting. Refusals come
// before anything is written: a bad name or harness, no work item, a live
// name, a live worker on the item, and a full workers tab ("at cap").
func Spawn(ctx context.Context, d Deps, o SpawnOptions) (Spawned, error) {
	if err := prepareSpawn(ctx, d, &o); err != nil {
		return Spawned{}, err
	}
	if o.Worktree {
		if err := createWorktree(d, &o); err != nil {
			return Spawned{}, err
		}
	}
	linkedDirs(&o)
	warn := seedTrust(d, o)
	s, err := spawnPlaced(ctx, d, o)
	if err != nil {
		dropWorktree(o)
		return Spawned{}, err
	}
	s.Worktree, s.Branch, s.Warning = o.wt.Path, o.wt.Branch, warn
	return s, nil
}

// spawnPlaced places the worker, opens its run and boots it.
func spawnPlaced(ctx context.Context, d Deps, o SpawnOptions) (Spawned, error) {
	var info Info
	var rf *runstore.RunFolder
	var lost []lostWorker
	err := withWorkers(d.Store, string(o.CallerPane), func(w *Workers) error {
		var err error
		info, rf, lost, err = placeWorker(ctx, d, o, w)
		return err
	})
	for _, l := range lost { // other callers' records, now no lock is held
		if l.info.CallerPane != string(o.CallerPane) {
			_ = withWorkers(d.Store, l.info.CallerPane, func(w *Workers) error { unplace(ctx, d, w, l.info, l.ob); return nil })
		}
	}
	if err != nil {
		return Spawned{}, err
	}
	if err := boot(ctx, d, o, info); err != nil {
		abandon(d, info.Run, rf, info)
		_ = withWorkers(d.Store, string(o.CallerPane), func(w *Workers) error {
			_, err := release(ctx, d.Herdr, w, herdr.PaneID(info.Pane))
			return err
		})
		return Spawned{}, err
	}
	return Spawned{Run: info.Run, Name: info.Name, Pane: info.Pane, Harness: info.Harness, Status: StatusStarting, Model: info.Model,
		Profile: info.Setup.Profile, Effort: info.Setup.Effort, Tools: info.Setup.Tools, MCP: info.Setup.MCP, Plugins: info.Setup.Plugins}, nil
}

// placeWorker runs under the caller's workers lock: lost workers are swept
// out of the record first (sweepLost, so they hold no slot and are never
// split), then the cap, the name and item checks, the row, the pane, and the
// pane's terminal pinned with it in session.json. A failure after the row
// opened abandons the run, closing a pane it got.
func placeWorker(ctx context.Context, d Deps, o SpawnOptions, w *Workers) (Info, *runstore.RunFolder, []lostWorker, error) {
	if err := prune(ctx, d.Herdr, w); err != nil {
		return Info{}, nil, nil, err
	}
	sweepLost(ctx, d, w)
	if err := checkCap(w); err != nil {
		return Info{}, nil, nil, err
	}
	info, rf, lost, err := claimName(ctx, d, o)
	for _, l := range lost {
		if l.info.CallerPane == string(o.CallerPane) {
			unplace(ctx, d, w, l.info, l.ob) // never split a lost worker's pane
		}
	}
	if err != nil {
		return Info{}, nil, lost, err
	}
	tabBefore := w.Tab
	pane, err := place(herdr.WithTerminalName(ctx, terminalName(info.Name, o.WorkTitle)), d.Herdr, w, info.Run, o.Cwd)
	if err != nil {
		abandon(d, info.Run, rf, Info{})
		return Info{}, nil, lost, fmt.Errorf("delegate: no pane for %s: %w", o.Name, err)
	}
	p, err := d.Herdr.Get(ctx, pane)
	if err != nil || p.TerminalID == "" {
		closeNewPane(ctx, d, w, tabBefore, pane)
		abandon(d, info.Run, rf, Info{})
		return Info{}, nil, lost, fmt.Errorf("delegate: herdr cannot name the terminal of new pane %s (%v); it was closed and nothing was typed", pane, err)
	}
	placed := Info{Run: info.Run, Name: info.Name, Pane: string(pane), TerminalID: p.TerminalID}
	saved, err := UpdateInfo(rf, func(i *Info) { i.Pane, i.TerminalID = placed.Pane, placed.TerminalID })
	if err != nil {
		abandon(d, info.Run, rf, placed)
		return Info{}, nil, lost, err
	}
	return saved, rf, lost, nil
}

// closeNewPane closes a pane this spawn just made, and the workers tab when
// this spawn made that too (w.Tab is not the one it started with), when its
// terminal cannot be pinned. The record is not saved on that error, so they
// would be tracked nowhere. Both were made in this call under the caller's
// lock and nothing was typed, so no pin check is needed.
func closeNewPane(ctx context.Context, d Deps, w *Workers, tabBefore *herdr.WorkersTab, pane herdr.PaneID) {
	_ = d.Herdr.Close(ctx, pane)
	if w.Tab != nil && w.Tab != tabBefore {
		_ = d.Herdr.TabClose(ctx, w.Tab.ID)
	}
}

// prepareSpawn resolves the work item (Deps.ResolveWorkItem, so a resume's
// defaulted item is resolved too), names an unnamed worker after it, and
// runs the refusals that need no lock.
func prepareSpawn(ctx context.Context, d Deps, o *SpawnOptions) error {
	if d.ResolveWorkItem != nil && o.WorkItem != "" {
		id, err := d.ResolveWorkItem(o.WorkItem)
		if err != nil {
			return err
		}
		o.WorkItem = id
	}
	if o.Name == "" {
		o.Name = slug.WorkerName(o.WorkItem, o.WorkTitle)
	}
	if err := harnessFlags(ctx, o); err != nil {
		return err
	}
	return checkSpawn(d, *o)
}

func checkSpawn(d Deps, o SpawnOptions) error {
	switch {
	case o.WorkItem == "":
		return fmt.Errorf("%w: pass --work-item or claim one first", ErrNoWorkItem)
	case !ValidName(o.Name):
		return fmt.Errorf("%w %q: use [a-z0-9-], 1 to 32 characters, starting with a letter", ErrBadName, o.Name)
	case o.Harness != HarnessClaude && o.Harness != HarnessCodex:
		return fmt.Errorf("%w, not %q", ErrBadHarness, o.Harness)
	case d.Herdr == nil:
		return ErrNotInHerdr
	case o.BootLine == nil:
		return errors.New("delegate: spawn has no boot line")
	}
	if err := checkWorktree(o); err != nil {
		return err
	}
	return checkContainerSpawn(o)
}

// namesLock is the project-wide lock that makes the name check and the
// ledger open one step, so two spawns of one name (from any callers) cannot
// both pass the check. It is held for the check and the open only, never
// across placing or booting the pane.
const namesLock = "names.lock"

// claimName checks that the name and the work item are free (checkLive: one
// live worker per item, slice 16) and opens the run, under namesLock.
// It also returns the lost workers checkLive closed.
func claimName(ctx context.Context, d Deps, o SpawnOptions) (Info, *runstore.RunFolder, []lostWorker, error) {
	if err := os.MkdirAll(d.Store.Root(), 0o700); err != nil {
		return Info{}, nil, nil, fmt.Errorf("delegate: run store: %w", err)
	}
	releaseNames := filelock.Guard(filepath.Join(d.Store.Root(), namesLock))
	defer releaseNames()
	if err := checkSessionFree(d, o.ResumedFrom, o.ResumeSession); err != nil {
		return Info{}, nil, nil, err // the resume refusal names the run to resume instead
	}
	lost, err := checkLive(ctx, d, o)
	if err != nil {
		return Info{}, nil, lost, err
	}
	info, rf, err := openRun(d, o)
	return info, rf, lost, err
}

// openRun opens the ledger row, then the run folder with session.json, the
// brief's task message and brief.md.
func openRun(d Deps, o SpawnOptions) (Info, *runstore.RunFolder, error) {
	row, err := d.Ledger.Open(delegationledger.Record{
		Mode: delegationledger.ModeInteractive, Harness: o.Harness, Name: o.Name, WorkItem: o.WorkItem,
		Link: "tag", CallerSession: o.CallerSession, Host: placeOf(o), Started: d.now(),
		ResumedFrom: o.ResumedFrom, Model: o.Model, Setup: o.setup().String(),
	})
	if err != nil {
		return Info{}, nil, err
	}
	info := Info{
		Run: row.ID, Name: o.Name, Harness: o.Harness, WorkItem: o.WorkItem, Caller: o.CallerSession,
		CallerPane: string(o.CallerPane), Cwd: o.Cwd, Place: placeOf(o), Created: d.now().Format(time.RFC3339),
		ResumedFrom: o.ResumedFrom, ResumeSession: o.ResumeSession, Model: o.Model, Flags: o.Flags, Lean: o.Lean, Setup: o.setup(),
	}
	if o.Container {
		info.Root = o.Root
	}
	info.Worktree, info.Branch, info.BaseSHA = o.wt.Path, o.wt.Branch, o.wt.BaseSHA
	info.SessionID, info.Transcript, info.WorkDir, info.ProjectDir = o.SessionID, o.Transcript, o.WorkDir, o.ProjectDir
	rf, err := d.Store.CreateRun(row.ID)
	if err == nil && o.LaunchArgs != nil {
		info.Args, err = o.LaunchArgs(rf)
	}
	if err == nil {
		err = writeMCPConfig(rf, &info, o.mcpConfig)
	}
	if err == nil {
		err = writeRunFiles(d, rf, &info, o.Brief)
	}
	if err != nil {
		abandon(d, row.ID, rf, Info{})
		return Info{}, nil, err
	}
	return info, rf, nil
}

func writeRunFiles(d Deps, rf *runstore.RunFolder, info *Info, brief string) error {
	if err := rf.WriteLabel(info.Name); err != nil {
		return err
	}
	if err := rf.WriteHost(runstore.HostHerdr); err != nil {
		return err
	}
	if err := rf.WriteStart(d.now()); err != nil {
		return err
	}
	if brief != "" {
		mb, err := d.Store.Mailbox(info.Name)
		if err != nil {
			return err
		}
		task, err := mb.Post(runstore.Inbox, runstore.Message{Run: info.Run, From: "caller", To: info.Name, Type: runstore.TypeTask, Body: brief})
		if err != nil {
			return err
		}
		info.Task = task.ID
		if err := rf.WriteFile(runstore.FileBrief, []byte(TaskPreamble(*info, mb.Dir())+brief+"\n")); err != nil {
			return err
		}
	}
	return WriteInfo(rf, *info)
}

// abandon closes a spawn that failed after its row was opened: a pane it got
// (placed: its pane and pinned terminal) is closed while it holds that
// terminal, the row ends gone and, when this call closed it, the run folder
// is finished.
func abandon(d Deps, run string, rf *runstore.RunFolder, placed Info) {
	closeOwnPane(context.Background(), d.Herdr, placed)
	closed, err := closeWorkerRow(d, run, delegationledger.EndGone)
	if err == nil && closed && rf != nil {
		_ = rf.Finish(1, d.now())
	}
}

// boot waits for the new pane's shell to settle, then types the boot line.
// It never types blind: an unsettled shell is ErrShellNotReady.
func boot(ctx context.Context, d Deps, o SpawnOptions, info Info) error {
	wait, poll := o.ShellWait, o.Poll
	if wait <= 0 {
		wait = DefaultShellWait
	}
	if poll <= 0 {
		poll = DefaultPoll
	}
	pane := herdr.PaneID(info.Pane)
	deadline := time.Now().Add(wait)
	for {
		pi, err := d.Herdr.Processes(ctx, pane)
		if err == nil && bareShell(pi) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%w within %s (pane %s); nothing was typed", ErrShellNotReady, wait, pane)
		}
		if err := sleepCtx(ctx, poll); err != nil {
			return err
		}
	}
	if ok, why := ownPane(ctx, d.Herdr, info); !ok {
		return fmt.Errorf("delegate: %s; the boot line was not typed", why)
	}
	if err := d.Herdr.Run(ctx, pane, o.BootLine(info.Run)); err != nil {
		return fmt.Errorf("delegate: herdr would not run the boot line in %s: %w", pane, err)
	}
	return nil
}

// bareShell is POSITIVELY a shell alone: herdr named the shell pid and every
// foreground process is it. Missing details are unknown, never a shell.
func bareShell(pi herdr.ProcessInfo) bool {
	if !pi.Known || pi.ShellPID == 0 || len(pi.Foreground) == 0 {
		return false
	}
	for _, p := range pi.Foreground {
		if p.PID != pi.ShellPID {
			return false
		}
	}
	return true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// writeMCPConfig writes the generated MCP config to the run folder, mode 0600
// (entries can hold secrets), and adds --mcp-config <path> to the flags. The
// path is on argv; the content never is.
func writeMCPConfig(rf *runstore.RunFolder, info *Info, config []byte) error {
	if len(config) == 0 {
		return nil
	}
	path := filepath.Join(rf.Dir(), "mcp.json")
	if err := os.WriteFile(path, config, 0o600); err != nil {
		return err
	}
	info.Flags = append(info.Flags, "--mcp-config", path)
	return nil
}
