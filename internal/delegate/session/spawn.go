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
	ShellWait                  time.Duration // DefaultShellWait when zero
	Poll                       time.Duration // DefaultPoll when zero
}

// Spawned is spawn's payload: {run, name, pane, harness, status}.
type Spawned struct {
	Run     string `json:"run"`
	Name    string `json:"name"`
	Pane    string `json:"pane"`
	Harness string `json:"harness"`
	Status  string `json:"status"`
}

// Spawn starts a worker and returns with status starting. Refusals come
// before anything is written: a bad name or harness, no work item, a live
// name, a live worker on the item, and a full workers tab ("at cap").
func Spawn(ctx context.Context, d Deps, o SpawnOptions) (Spawned, error) {
	if err := prepareSpawn(d, &o); err != nil {
		return Spawned{}, err
	}
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
	return Spawned{Run: info.Run, Name: info.Name, Pane: info.Pane, Harness: info.Harness, Status: StatusStarting}, nil
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
	pane, err := place(ctx, d.Herdr, w, info.Run, o.Cwd)
	if err != nil {
		abandon(d, info.Run, rf, Info{})
		return Info{}, nil, lost, fmt.Errorf("delegate: no pane for %s: %w", o.Name, err)
	}
	p, err := d.Herdr.Get(ctx, pane)
	if err != nil || p.TerminalID == "" {
		abandon(d, info.Run, rf, Info{})
		return Info{}, nil, lost, fmt.Errorf("delegate: herdr cannot name the terminal of new pane %s (%v); it is left open and nothing was typed", pane, err)
	}
	placed := Info{Run: info.Run, Name: info.Name, Pane: string(pane), TerminalID: p.TerminalID}
	saved, err := UpdateInfo(rf, func(i *Info) { i.Pane, i.TerminalID = placed.Pane, placed.TerminalID })
	if err != nil {
		abandon(d, info.Run, rf, placed)
		return Info{}, nil, lost, err
	}
	return saved, rf, lost, nil
}

// prepareSpawn resolves the work item (Deps.ResolveWorkItem, so a resume's
// defaulted item is resolved too), names an unnamed worker after it, and
// runs the refusals that need no lock.
func prepareSpawn(d Deps, o *SpawnOptions) error {
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
		ResumedFrom: o.ResumedFrom,
	})
	if err != nil {
		return Info{}, nil, err
	}
	info := Info{
		Run: row.ID, Name: o.Name, Harness: o.Harness, WorkItem: o.WorkItem, Caller: o.CallerSession,
		CallerPane: string(o.CallerPane), Cwd: o.Cwd, Place: placeOf(o), Created: d.now().Format(time.RFC3339),
		ResumedFrom: o.ResumedFrom, ResumeSession: o.ResumeSession,
	}
	if o.Container {
		info.Root = o.Root
	}
	rf, err := d.Store.CreateRun(row.ID)
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
