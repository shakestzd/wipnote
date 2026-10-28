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
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Spawn waits.
const (
	DefaultShellWait = 2 * time.Second
	DefaultPoll      = 100 * time.Millisecond
)

// SpawnOptions is one spawn. WorkItem is required: the caller resolves the
// default (the active claim) before calling.
type SpawnOptions struct {
	Name, Harness, WorkItem string
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
// name, and a full workers tab ("at cap").
func Spawn(ctx context.Context, d Deps, o SpawnOptions) (Spawned, error) {
	if err := checkSpawn(d, o); err != nil {
		return Spawned{}, err
	}
	var info Info
	var rf *runstore.RunFolder
	err := withWorkers(d.Store, string(o.CallerPane), func(w *Workers) error {
		if err := prune(ctx, d.Herdr, w); err != nil {
			return err
		}
		if err := checkCap(w); err != nil {
			return err
		}
		var err error
		if info, rf, err = claimName(ctx, d, o); err != nil {
			return err
		}
		pane, err := place(ctx, d.Herdr, w, info.Run, o.Cwd)
		if err != nil {
			abandon(d, info.Run, rf, "")
			return fmt.Errorf("delegate: no pane for %s: %w", o.Name, err)
		}
		info.Pane = string(pane)
		return WriteInfo(rf, info)
	})
	if err != nil {
		return Spawned{}, err
	}
	if err := boot(ctx, d, o, info); err != nil {
		abandon(d, info.Run, rf, herdr.PaneID(info.Pane))
		_ = withWorkers(d.Store, string(o.CallerPane), func(w *Workers) error {
			_, err := release(ctx, d.Herdr, w, herdr.PaneID(info.Pane))
			return err
		})
		return Spawned{}, err
	}
	return Spawned{Run: info.Run, Name: info.Name, Pane: info.Pane, Harness: info.Harness, Status: StatusStarting}, nil
}

func checkSpawn(d Deps, o SpawnOptions) error {
	switch {
	case !ValidName(o.Name):
		return fmt.Errorf("%w %q: use [a-z0-9-], 1 to 32 characters, starting with a letter", ErrBadName, o.Name)
	case o.Harness != HarnessClaude && o.Harness != HarnessCodex:
		return fmt.Errorf("%w, not %q", ErrBadHarness, o.Harness)
	case o.WorkItem == "":
		return fmt.Errorf("%w: pass --work-item or claim one first", ErrNoWorkItem)
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

// claimName checks that name is free and opens the run, under namesLock.
func claimName(ctx context.Context, d Deps, o SpawnOptions) (Info, *runstore.RunFolder, error) {
	if err := os.MkdirAll(d.Store.Root(), 0o700); err != nil {
		return Info{}, nil, fmt.Errorf("delegate: run store: %w", err)
	}
	releaseNames := filelock.Guard(filepath.Join(d.Store.Root(), namesLock))
	defer releaseNames()
	if err := freeName(ctx, d, o.Name); err != nil {
		return Info{}, nil, err
	}
	if err := checkResumedOnce(d, o.ResumedFrom); err != nil {
		return Info{}, nil, err
	}
	return openRun(d, o)
}

// freeName refuses a name an open interactive row holds while its pane
// lives. A row whose pane herdr says is gone is closed first (end gone).
func freeName(ctx context.Context, d Deps, name string) error {
	open, ok := d.Ledger.ReadOpen()
	if !ok {
		return errors.New("delegate: the delegations ledger cannot be read; nothing was spawned")
	}
	for _, r := range open.Interactive() {
		if r.Name != name {
			continue
		}
		_, info, err := loadInfo(d.Store, r.ID)
		if err == nil && info.Pane != "" {
			if _, gerr := d.Herdr.Get(ctx, herdr.PaneID(info.Pane)); herdr.IsAbsent(gerr) {
				_, _ = d.Ledger.CloseIfOpen(r.ID, delegationledger.CloseInfo{Ended: d.now(), Reason: delegationledger.EndGone})
				continue
			}
		}
		return fmt.Errorf("%w: %s is run %s", ErrLiveName, name, r.ID)
	}
	return nil
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
		abandon(d, row.ID, rf, "")
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
		task, err := mb.Post(runstore.Inbox, runstore.Message{From: "caller", To: info.Name, Type: runstore.TypeTask, Body: brief})
		if err != nil {
			return err
		}
		info.Task = task.ID
		if err := rf.WriteFile(runstore.FileBrief, []byte(Preamble(*info, mb.Dir())+brief+"\n")); err != nil {
			return err
		}
	}
	return WriteInfo(rf, *info)
}

// abandon closes a spawn that failed after its row was opened: the row ends
// gone, the run folder is finished, and a pane it got is closed.
func abandon(d Deps, run string, rf *runstore.RunFolder, pane herdr.PaneID) {
	if pane != "" && d.Herdr != nil {
		_ = d.Herdr.Close(context.Background(), pane)
	}
	if rf != nil {
		_ = rf.Finish(1, d.now())
	}
	_, _ = d.Ledger.CloseIfOpen(run, delegationledger.CloseInfo{Ended: d.now(), Reason: delegationledger.EndGone})
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
