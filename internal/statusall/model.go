// Package statusall builds `wipnote status --all`: one row per registered
// project that answers where the project is (plan, slice, items in progress,
// last commit, PR and CI), what each agent is doing and why (its work item),
// and what waits on the owner (needs_you). See docs/design/status-all.md.
//
// It is read-only: it reads the canonical .wipnote files and ledgers directly
// (no SQLite), runs git and gh read commands, and lists herdr agents. Every
// external call has a timeout, a missing gh or herdr marks only its own part
// "not available", and a project that cannot be read becomes an error row.
// It never runs inside a hook.
package statusall

import (
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/procsweep"
)

// Schema names the JSON shape. A breaking change bumps the suffix.
const Schema = "wipnote.status-all/v1"

// Source states in Project.Sources.
const (
	SourceOK      = "ok"
	SourceSkipped = "skipped"
	SourceNone    = "none"
	// SourceNotAvailable prefixes the reason a part could not be read.
	SourceNotAvailable = "not available"
)

// Needs-you kinds.
const (
	NeedBlockedAgent  = "blocked_agent"
	NeedFailedRun     = "failed_run"
	NeedLostRun       = "lost_run"
	NeedOrphanProcess = "orphan_process"
)

// Report is the whole `status --all --json` document.
type Report struct {
	Schema      string    `json:"schema"`
	GeneratedAt string    `json:"generated_at"`
	Projects    []Project `json:"projects"`
}

// Project is one registered project's row.
type Project struct {
	Name         string            `json:"name"`
	ID           string            `json:"id"`
	LastActivity string            `json:"last_activity,omitempty"`
	Branch       string            `json:"branch,omitempty"`
	Plan         *Plan             `json:"plan"`
	InProgress   []Item            `json:"in_progress"`
	LastCommit   *Commit           `json:"last_commit"`
	PR           *PR               `json:"pr"`
	Agents       []Agent           `json:"agents"`
	Runs         []Run             `json:"runs"`
	NeedsYou     []Need            `json:"needs_you"`
	Orphans      []procsweep.Proc  `json:"orphans"`
	Sources      map[string]string `json:"sources"`
	Errors       []string          `json:"errors"`
}

// Plan is the project's active plan and its current slice.
type Plan struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Slice *Slice `json:"slice"`
	Done  int    `json:"done"`
	Total int    `json:"total"`
}

// Slice is the plan's current slice.
type Slice struct {
	Num       int    `json:"num"`
	Title     string `json:"title"`
	FeatureID string `json:"feature_id,omitempty"`
	Status    string `json:"status"`
}

// Item is a work item in progress.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// Commit is the last commit on the project's checked-out branch.
type Commit struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	At      string `json:"at"`
}

// PR is the pull request for the project's current branch.
type PR struct {
	Number    int             `json:"number"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	URL       string          `json:"url"`
	Checks    gitwatch.Counts `json:"checks"`
	Failing   []string        `json:"failing"`
	Review    string          `json:"review"`
	Mergeable string          `json:"mergeable"`
}

// Agent is one live agent: a herdr pane, a claim in the ledgers, or both.
type Agent struct {
	Source   string `json:"source"` // herdr | ledger | herdr+ledger
	Harness  string `json:"harness"`
	State    string `json:"state"` // herdr state, or "claimed" when only the ledger knows it
	WorkItem string `json:"work_item,omitempty"`
	Title    string `json:"title,omitempty"`
	LastLine string `json:"last_line,omitempty"`
	Pane     string `json:"pane,omitempty"`
	Session  string `json:"session,omitempty"`
	Since    string `json:"since,omitempty"`
}

// Run is one recent background run (core/runledger), newest first.
type Run struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
	Command     string `json:"command,omitempty"`
	Folder      string `json:"folder,omitempty"`
	Status      string `json:"status"`
	Exit        *int   `json:"exit_code,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	EndedAt     string `json:"ended_at,omitempty"`
	Commit      string `json:"commit,omitempty"`
	LogPath     string `json:"log_path,omitempty"`
	LastLine    string `json:"last_line,omitempty"`
	FirstError  string `json:"first_error,omitempty"`
	WorkItem    string `json:"work_item,omitempty"`
	Session     string `json:"session,omitempty"`
	Harness     string `json:"harness,omitempty"`
	Line        string `json:"line"`
}

// Need is one thing waiting on the owner.
type Need struct {
	Kind     string `json:"kind"`
	What     string `json:"what"`
	Since    string `json:"since,omitempty"`
	WorkItem string `json:"work_item,omitempty"`
}
