// Package delegationledger is the canonical, git-tracked record of delegation
// runs: one row per run in .wipnote/delegations-ledger.html (feat-dd8b0cde,
// plan-28b4a7f6 slice 4).
//
// # One row type for both run modes (Q4)
//
// A one-shot run (`codex exec`, `claude -p`, `ccx task`) and an interactive
// worker session share every column that matters to a reader: harness, label,
// work item, times, exit and usage. mode tells them apart.
//
// # Lifecycle (Q3)
//
// Spawn and `delegate run` call Open, which appends at the tail. Exit, stop or
// pane loss call Close, which stamps the end once. The usage parser and the
// session-id hooks call Enrich. Close and Enrich are read-modify-writes under
// core/filelock.Guard with temp file plus rename, as in core/sessionledger.
// Tool, prompt and turn events are NOT copied here: they duplicate agent_events.
//
// # What never enters a row
//
// No absolute host path, user name or home directory: every string column is
// checked by core/hostpath at write time. A worker's cwd and run folder are
// host paths, so they stay in the gitignored run folder.
package delegationledger

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/hostpath"
)

// FileName is the git-tracked canonical ledger beside sessions-ledger.html and
// gate-ledger.html.
const FileName = "delegations-ledger.html"

// TimeFormat is RFC3339 in UTC with a fixed nine-digit fraction, matching the
// sibling ledgers so the derived index can compare timestamps as TEXT.
const TimeFormat = "2006-01-02T15:04:05.000000000Z"

// Run modes.
const (
	ModeOneshot     = "oneshot"
	ModeInteractive = "interactive"
)

// End reasons. EndImported marks rows copied from orch history (slice 18).
const (
	EndExit       = "exit"
	EndStop       = "stop"
	EndPaneClosed = "pane_closed"
	EndGone       = "gone"
	EndImported   = "imported"
)

var (
	modes      = set(ModeOneshot, ModeInteractive)
	harnesses  = set("claude", "claude-print", "codex", "codex-review", "ccx")
	links      = set("tag", "sid")
	hosts      = set("host", "container")
	endReasons = set(EndExit, EndStop, EndPaneClosed, EndGone, EndImported)

	// runIDPattern: at least 12 hex (48 bits). orch's 8 hex collided and
	// pointed a worker at an older task (orch P-6).
	runIDPattern    = regexp.MustCompile(`^dlg-[0-9a-f]{12,}$`)
	workItemPattern = regexp.MustCompile(`^(feat|bug|spk)-[0-9a-z]+$`)
)

func set(vs ...string) map[string]bool {
	m := make(map[string]bool, len(vs))
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// Record is one delegation run. Column meanings follow the plan's data-model
// block; empty strings and a nil Exit mean "not recorded".
type Record struct {
	ID            string // dlg-<12 hex>, also the row's fragment id
	Mode          string // oneshot | interactive
	Harness       string // claude | claude-print | codex | codex-review | ccx
	Name          string // interactive runs only
	Label         string // an explicit --label only; never derived
	WorkItem      string // feat-/bug-/spk- id or empty
	Link          string // tag | sid
	CallerSession string
	// WorkerSessions holds the harness's own session ids (Claude session id,
	// Codex thread id), oldest first. /clear gives a new id, so an interactive
	// run can hold several; the claim gate matches episodes against every one,
	// and resume (slice 20) relaunches the last.
	WorkerSessions []string
	Host           string // host | container
	Started        time.Time
	Ended          time.Time
	Exit           *int
	EndReason      string
	UsageInput     int
	UsageCached    int
	UsageOutput    int
	// UsageReasoning is Codex's reasoning_output_tokens (slice 12); 0 for
	// harnesses that do not report it and for rows written before it.
	UsageReasoning int
	ResumedFrom    string // dlg- id of the run this one resumed
}

// IsOpen reports whether the run has no recorded end. An imported row
// (end_reason=imported) is closed even when orch left no end time.
func (r Record) IsOpen() bool { return r.Ended.IsZero() && r.EndReason == "" }

// LastWorkerSession returns the newest worker session id, or "".
func (r Record) LastWorkerSession() string {
	if len(r.WorkerSessions) == 0 {
		return ""
	}
	return r.WorkerSessions[len(r.WorkerSessions)-1]
}

// NewRunID mints a run id with 12 hex characters (48 bits).
func NewRunID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("delegationledger: crypto/rand: %v", err))
	}
	return "dlg-" + hex.EncodeToString(b[:])
}

// FormatTime renders t in the on-disk format.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

// ParseTime parses an on-disk timestamp; "" is the zero time.
func ParseTime(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return time.Time{}, nil
	}
	return time.Parse(time.RFC3339Nano, v)
}

// Validate refuses a row that breaks the data model or carries a host path.
func (r Record) Validate() error {
	if !runIDPattern.MatchString(r.ID) {
		return fmt.Errorf("delegationledger: run id %q is not dlg-<12+ hex>", r.ID)
	}
	if err := r.validateEnums(); err != nil {
		return err
	}
	if r.Started.IsZero() {
		return fmt.Errorf("delegationledger: run %s missing started", r.ID)
	}
	if r.Ended.IsZero() != (r.EndReason == "") && r.EndReason != EndImported {
		return fmt.Errorf("delegationledger: run %s: ended and end_reason must be set together", r.ID)
	}
	if !r.Ended.IsZero() && r.Ended.Before(r.Started) {
		return fmt.Errorf("delegationledger: run %s ends before it starts", r.ID)
	}
	if r.Name != "" && r.Mode != ModeInteractive {
		return fmt.Errorf("delegationledger: run %s: name is for interactive runs only", r.ID)
	}
	if r.ResumedFrom != "" && !runIDPattern.MatchString(r.ResumedFrom) {
		return fmt.Errorf("delegationledger: run %s: resumed_from %q is not a run id", r.ID, r.ResumedFrom)
	}
	return r.checkHostPaths()
}

func (r Record) validateEnums() error {
	checks := []struct {
		field, value string
		allowed      map[string]bool
		required     bool
	}{
		{"mode", r.Mode, modes, true},
		{"harness", r.Harness, harnesses, true},
		{"link", r.Link, links, false},
		{"host", r.Host, hosts, false},
		{"end_reason", r.EndReason, endReasons, false},
	}
	for _, c := range checks {
		if c.value == "" && !c.required {
			continue
		}
		if !c.allowed[c.value] {
			return fmt.Errorf("delegationledger: run %s: invalid %s %q", r.ID, c.field, c.value)
		}
	}
	if r.WorkItem != "" && !workItemPattern.MatchString(r.WorkItem) {
		return fmt.Errorf("delegationledger: run %s: work_item %q is not a feat-/bug-/spk- id", r.ID, r.WorkItem)
	}
	return nil
}

// checkHostPaths runs the shared detector over every free-text column.
func (r Record) checkHostPaths() error {
	fields := map[string]string{
		"name": r.Name, "label": r.Label, "caller_session": r.CallerSession,
	}
	for i, s := range r.WorkerSessions {
		// The column is space-separated, so an id must be one token.
		if s == "" || strings.ContainsAny(s, " \t\r\n") {
			return fmt.Errorf("delegationledger: run %s: worker session %q is not one token", r.ID, s)
		}
		fields[fmt.Sprintf("worker_sessions[%d]", i)] = s
	}
	for field, v := range fields {
		if err := hostpath.Check(field, v); err != nil {
			return fmt.Errorf("delegationledger: run %s: %w", r.ID, err)
		}
	}
	return nil
}
