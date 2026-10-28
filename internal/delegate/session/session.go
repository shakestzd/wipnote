// Package session drives interactive wipnote delegate workers (plan-28b4a7f6
// slice 10): Claude or Codex sessions started under wipnote's own launchers in
// herdr panes, which the caller and a person can both talk to.
//
// # Verbs
//
//   - Spawn places a worker pane in the caller's workers tab, waits for the
//     pane's shell to settle, types ONE short boot line and returns with
//     status "starting" (decision F). It never types the brief: the brief goes
//     by file in the run folder (finding 3).
//   - Boot runs in the pane: BootArgv and BootEnv turn the run folder into the
//     launcher's argv and a scrubbed environment (the worker is its own root).
//   - Ready is the one readiness predicate that probe, send and wait share
//     (D2). Working comes from hook events, never from input-line text (D8).
//   - Stop records the worker's pids, asks the agent to exit, closes the pane
//     only at a positively bare shell, and reports survivors. It never
//     signals or kills (D5, finding 2).
//   - List observes every run (renaming and pinning a late agent and moving
//     bound sessions into the ledger), and Tree renders caller, runs, worker
//     sessions and sub-agents.
//
// # State
//
// The ledger row (core/delegationledger, mode interactive) is the canonical
// record. The run folder (internal/delegate/runstore) holds the host-local
// facts: session.json (Info), brief.md, and the events the worker's hooks
// write. The caller's workers tab and its panes are kept in
// <store>/workers/<caller-pane>.json. Nothing here runs in the background:
// every verb is a short process with bounded waits.
package session

import (
	"errors"
	"regexp"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Harnesses a worker can run under.
const (
	HarnessClaude = "claude"
	HarnessCodex  = "codex"
)

// Environment a worker carries. RunEnv names its run, CallerEnv the caller's
// session (the ledger's caller_session). Hooks read RunEnv to write events.
const (
	RunEnv    = "WIPNOTE_DELEGATE_RUN"
	CallerEnv = "WIPNOTE_DELEGATE_CALLER"
)

// StatusStarting is what Spawn returns: the launch, bind and rename happen in
// the pane or on a later list, not in spawn.
const StatusStarting = "starting"

// Refusals. Test them with errors.Is.
var (
	ErrBadName       = errors.New("delegate: invalid worker name")
	ErrBadHarness    = errors.New("delegate: harness must be claude or codex")
	ErrNoWorkItem    = errors.New("delegate: no work item")
	ErrLiveName      = errors.New("delegate: a live worker already has this name")
	ErrAtCap         = errors.New("delegate: at cap")
	ErrNotInHerdr    = errors.New("delegate: interactive workers need herdr (HERDR_ENV=1)")
	ErrShellNotReady = errors.New("delegate: the new pane's shell did not settle")
	ErrNoWorker      = errors.New("delegate: no live worker by that name")
)

// namePattern: [a-z0-9-], 1 to 32 characters, starting with a letter so the
// name is also a valid mailbox name (runstore.ValidName).
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidName reports whether name is a worker name.
func ValidName(name string) bool { return namePattern.MatchString(name) && runstore.ValidName(name) }

// Deps are the stores and clients every verb uses. Herdr may be nil outside
// herdr: List and Tree then read files only.
type Deps struct {
	Herdr  herdr.Client
	Store  *runstore.Store
	Ledger *delegationledger.Store
	// Now is the clock; time.Now when nil.
	Now func() time.Time
	// ProcStart reports a pid's start time, ok false when no such process
	// runs. Nil uses ps (procs.go). Stop compares start times so a reused pid
	// is not a survivor.
	ProcStart func(pid int) (string, bool)
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now().UTC()
	}
	return time.Now().UTC()
}

func (d Deps) procStart(pid int) (string, bool) {
	if d.ProcStart != nil {
		return d.ProcStart(pid)
	}
	return psStart(pid)
}
