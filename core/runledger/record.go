// Package runledger is the canonical, git-tracked record of the background
// tasks an agent starts: background shells, subagents, monitors, and scripts
// run through `wipnote run` (feat-36658c22, plan-56a4f0ad slice 1).
//
// # Read the harness's records; never wrap
//
// The agent's command is never rewritten. A Claude Code start comes from the
// PostToolUse payload of the call that launched the task (task id, output
// file), and its end from the <task-notification> Claude Code writes to the
// session transcript, which the Stop and UserPromptSubmit hooks read from a
// saved offset. A Codex shell call starts at PreToolUse and ends at its late
// PostToolUse. `wipnote run` is an optional wrapper for scripts run outside a
// harness.
//
// # Storage: an append-only event log, one shard per writer
//
// .wipnote/runs/sess-<session>.html holds one <tr> per EVENT (a start or an
// end), not one per run. A hook therefore only ever appends a line — no
// read-modify-write, no SQLite — and the reader folds the events into Records
// (Fold). Each session writes its own shard, as core/claimledger does, so
// concurrent sessions and worktrees never write the same file.
//
// # Exit code caveat
//
// The exit code is the OUTER command's: a gate that ends in `echo` reports 0
// even when a step failed. The end event therefore also keeps the first error
// line of the output file.
//
// # What never enters a row
//
// No absolute host path, user name or home directory: Clean, Folder and
// StoredPath make values relative or redacted, and Validate refuses the rest
// through core/hostpath.
package runledger

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/hostpath"
)

// DirName is the git-tracked directory holding the run shards.
const DirName = "runs"

// TimeFormat matches the sibling ledgers: RFC3339 UTC, fixed nine digits.
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// Event phases.
const (
	PhaseStart = "start"
	PhaseEnd   = "end"
)

// Run kinds.
const (
	KindShell    = "shell"
	KindSubagent = "subagent"
	KindMonitor  = "monitor"
	KindWrapper  = "wrapper"
)

// Run statuses. Running and unknown are derived by Fold, never written.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusKilled    = "killed"
	StatusUnknown   = "unknown"
)

var (
	phases       = set(PhaseStart, PhaseEnd)
	kinds        = set(KindShell, KindSubagent, KindMonitor, KindWrapper)
	endStatuses  = set(StatusCompleted, StatusFailed, StatusKilled, StatusUnknown)
	runIDPattern = regexp.MustCompile(`^run-[0-9a-f]{12}$`)
	workItemPat  = regexp.MustCompile(`^(feat|bug|spk)-[0-9a-z]+$`)
)

func set(vs ...string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// Event is one appended row: the start or the end of a run. Empty strings and
// a nil Exit mean "not known from this event".
type Event struct {
	RunID       string // run-<12 hex>; the same for every event of one run
	Phase       string // start | end
	At          time.Time
	TaskID      string // the harness's task id (Claude background id, agent id)
	ToolUseID   string
	Kind        string // shell | subagent | monitor | wrapper
	Description string
	Command     string
	Folder      string // relative to the project root
	Status      string // end only: completed | failed | killed | unknown
	Exit        *int   // end only
	LogPath     string // relative to the project root, or "…/<dir>/<file>"
	LastLine    string
	FirstError  string
	Commit      string
	Session     string
	WorkItem    string
	Harness     string
}

// RunIDFor derives the run id from the session and a per-run key (the
// tool-use id, or "task:<id>" when only the task id is known), so a start and
// an end written by different hooks name the same run.
func RunIDFor(session, key string) string {
	sum := sha256.Sum256([]byte(session + "\x00" + key))
	return "run-" + hex.EncodeToString(sum[:6])
}

// NewRunID mints a random run id, for wrapper runs.
func NewRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("runledger: crypto/rand: %v", err))
	}
	return "run-" + hex.EncodeToString(b[:])
}

// FormatTime renders t in the on-disk format.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// Validate refuses an event that breaks the data model or carries a host path.
func (e Event) Validate() error {
	if !runIDPattern.MatchString(e.RunID) {
		return fmt.Errorf("runledger: run id %q is not run-<12 hex>", e.RunID)
	}
	if !phases[e.Phase] {
		return fmt.Errorf("runledger: run %s: invalid phase %q", e.RunID, e.Phase)
	}
	if e.At.IsZero() {
		return fmt.Errorf("runledger: run %s: missing time", e.RunID)
	}
	if e.Kind != "" && !kinds[e.Kind] {
		return fmt.Errorf("runledger: run %s: invalid kind %q", e.RunID, e.Kind)
	}
	if e.Phase == PhaseEnd && !endStatuses[e.Status] {
		return fmt.Errorf("runledger: run %s: invalid end status %q", e.RunID, e.Status)
	}
	if e.Phase == PhaseStart && (e.Status != "" || e.Exit != nil) {
		return fmt.Errorf("runledger: run %s: a start carries no status or exit", e.RunID)
	}
	if e.WorkItem != "" && !workItemPat.MatchString(e.WorkItem) {
		return fmt.Errorf("runledger: run %s: work item %q is not a feat-/bug-/spk- id", e.RunID, e.WorkItem)
	}
	if strings.ContainsAny(e.Session, " \t\r\n") {
		return fmt.Errorf("runledger: run %s: session %q is not one token", e.RunID, e.Session)
	}
	return e.checkHostPaths()
}

func (e Event) checkHostPaths() error {
	fields := map[string]string{
		"description": e.Description, "command": e.Command, "folder": e.Folder,
		"log_path": e.LogPath, "last_line": e.LastLine, "first_error": e.FirstError,
		"task_id": e.TaskID,
	}
	for field, v := range fields {
		if err := hostpath.Check(field, v); err != nil {
			return fmt.Errorf("runledger: run %s: %w", e.RunID, err)
		}
	}
	return nil
}

// MapStatus turns the harness's status word and the exit code into a run
// status. A nonzero exit is a failure whatever the word says.
func MapStatus(word string, exit *int) string {
	if exit != nil && *exit != 0 {
		return StatusFailed
	}
	switch strings.ToLower(strings.TrimSpace(word)) {
	case "completed", "finished", "done", "success", "succeeded":
		return StatusCompleted
	case "failed", "error", "errored":
		return StatusFailed
	case "killed", "stopped", "cancelled", "canceled", "aborted", "interrupted":
		return StatusKilled
	}
	return StatusUnknown
}
