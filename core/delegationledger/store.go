package delegationledger

import (
	"errors"
	"fmt"
	"github.com/shakestzd/wipnote/core/containerwrite"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// ErrNoRow is returned by Close and Enrich when no row has the run id.
var ErrNoRow = errors.New("delegationledger: no row for run")

// OnCommit, when non-nil, is called after a successful write with the wipnote
// dir, the repo-relative ledger path and the action verb. cmd/wipnote sets it
// to the commit-queue producer (core may not import internal/commitqueue), so
// rows go through the existing artifact commit path and no write waits on the
// daemon.
var OnCommit func(wipnoteDir, relPath, action string)

// Store reads and writes the delegations ledger for one project.
type Store struct {
	wipnoteDir string
	path       string
}

// NewStore returns a Store for the ledger under wipnoteDir. The file is created
// on first write, so a reader never leaves an empty ledger behind.
func NewStore(wipnoteDir string) *Store {
	return &Store{wipnoteDir: wipnoteDir, path: filepath.Join(wipnoteDir, FileName)}
}

// Path returns the absolute ledger path.
func (s *Store) Path() string { return s.path }

// RelPath returns the ledger path relative to the repo root, for commits.
func (s *Store) RelPath() string {
	if rel, err := filepath.Rel(filepath.Dir(s.wipnoteDir), s.path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(s.wipnoteDir), FileName))
}

// ReadAll returns every run ordered by start. A missing ledger is empty.
func (s *Store) ReadAll() ([]Record, error) {
	recs, err := ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return recs, nil
}

// Get returns the row for one run.
func (s *Store) Get(runID string) (Record, bool, error) {
	recs, err := s.ReadAll()
	if err != nil {
		return Record{}, false, err
	}
	if i := indexOf(recs, runID); i >= 0 {
		return recs[i], true, nil
	}
	return Record{}, false, nil
}

// Open appends one run row and returns it. An empty ID is minted and a zero
// Started is set to now. A row is written exactly as given otherwise, so an
// import (slice 18) can write an already-closed row. A duplicate run id is
// refused rather than appended a second time.
func (s *Store) Open(r Record) (Record, error) {
	if r.ID == "" {
		r.ID = NewRunID()
	}
	if r.Started.IsZero() {
		r.Started = time.Now().UTC()
	}
	if err := r.Validate(); err != nil {
		return Record{}, err
	}
	if containerwrite.Active() {
		return Record{}, ErrContainerWorker
	}

	release := filelock.Guard(s.path)
	defer release()

	existing, err := s.ReadAll()
	if err != nil {
		return Record{}, err
	}
	if indexOf(existing, r.ID) >= 0 {
		return Record{}, fmt.Errorf("delegationledger: run %s already recorded", r.ID)
	}
	if err := appendRowLocked(s.path, r); err != nil {
		return Record{}, err
	}
	s.notify("delegation open")
	return r, nil
}

// CloseInfo is how a run ended. Reason is required; Exit is nil when the
// worker left no exit status (a stop or a lost pane).
type CloseInfo struct {
	Ended  time.Time
	Exit   *int
	Reason string
}

// Close records the end of a run in place. The first recorded end is the true
// one: closing an already-closed row changes nothing. Returns ErrNoRow when the
// run has no row.
func (s *Store) Close(runID string, c CloseInfo) error {
	_, err := s.CloseIfOpen(runID, c)
	return err
}

// CloseIfOpen is Close that also reports whether this call closed the row:
// false when the row was already closed, decided under the ledger lock.
func (s *Store) CloseIfOpen(runID string, c CloseInfo) (bool, error) {
	if c.Reason == "" {
		return false, fmt.Errorf("delegationledger: close %s: end reason required", runID)
	}
	if c.Ended.IsZero() {
		c.Ended = time.Now().UTC()
	}
	closed := false
	err := s.update(runID, "delegation close", func(r *Record) bool {
		if !r.IsOpen() {
			return false
		}
		if c.Ended.Before(r.Started) {
			c.Ended = r.Started
		}
		r.Ended, r.Exit, r.EndReason = c.Ended, c.Exit, c.Reason
		closed = true
		return true
	})
	return closed && err == nil, err
}

// Enrichment is what a later pass learned about a run. WorkerSessions are
// appended in order, skipping ids the row already holds. A non-nil usage field
// replaces the recorded count: the usage parser reports the run's totals.
type Enrichment struct {
	WorkerSessions []string
	UsageInput     *int
	UsageCached    *int
	UsageOutput    *int
	UsageReasoning *int
}

// Enrich applies e to a run's row and reports whether the row changed.
// Returns ErrNoRow when the run has no row.
func (s *Store) Enrich(runID string, e Enrichment) (bool, error) {
	changed := false
	err := s.update(runID, "delegation enrich", func(r *Record) bool {
		changed = applyEnrichment(r, e)
		return changed
	})
	return changed, err
}

// EnrichIfOpen is Enrich that changes nothing once the row is closed,
// decided under the ledger lock, so a late write (delegate wait adding a
// /clear session) never touches a stopped row: a stop always wins (orch F-1).
func (s *Store) EnrichIfOpen(runID string, e Enrichment) (bool, error) {
	changed := false
	err := s.update(runID, "delegation enrich", func(r *Record) bool {
		changed = r.IsOpen() && applyEnrichment(r, e)
		return changed
	})
	return changed, err
}

func applyEnrichment(r *Record, e Enrichment) bool {
	changed := false
	for _, sid := range e.WorkerSessions {
		if !slices.Contains(r.WorkerSessions, sid) {
			r.WorkerSessions = append(r.WorkerSessions, sid)
			changed = true
		}
	}
	for _, u := range []struct {
		dst *int
		src *int
	}{{&r.UsageInput, e.UsageInput}, {&r.UsageCached, e.UsageCached}, {&r.UsageOutput, e.UsageOutput}, {&r.UsageReasoning, e.UsageReasoning}} {
		if u.src != nil && *u.dst != *u.src {
			*u.dst = *u.src
			changed = true
		}
	}
	return changed
}

// update is the read-modify-write under the guard. mutate reports whether it
// changed the row; an unchanged row is not rewritten. The mutated row is
// validated before anything is written.
func (s *Store) update(runID, action string, mutate func(*Record) bool) error {
	if containerwrite.Active() {
		return ErrContainerWorker
	}
	release := filelock.Guard(s.path)
	defer release()

	records, err := s.ReadAll()
	if err != nil {
		return err
	}
	i := indexOf(records, runID)
	if i < 0 {
		return ErrNoRow
	}
	row := records[i]
	row.WorkerSessions = append([]string(nil), row.WorkerSessions...)
	if !mutate(&row) {
		return nil
	}
	if err := row.Validate(); err != nil {
		return err
	}
	records[i] = row
	if err := writeAllLocked(s.path, records); err != nil {
		return err
	}
	s.notify(action)
	return nil
}

// ErrContainerWorker refuses a write from inside a container worker: it
// writes only its own files, never the shared ledger (plan-28b4a7f6 slice 21).
var ErrContainerWorker = errors.New("delegationledger: a container worker does not write the shared delegations ledger")

// OpenRuns is one read of the ledger's open rows, for the claim gate and the
// delegate verbs. One read answers both questions they ask.
type OpenRuns struct{ rows []Record }

// ReadOpen reads the open rows. ok is false when the ledger exists but cannot
// be read, so the claim gate fails open as it does for the claim ledger. A
// missing ledger is readable and empty.
func (s *Store) ReadOpen() (OpenRuns, bool) {
	recs, err := s.ReadAll()
	if err != nil {
		return OpenRuns{}, false
	}
	var open []Record
	for _, r := range recs {
		if r.IsOpen() {
			open = append(open, r)
		}
	}
	return OpenRuns{rows: open}, true
}

// InteractiveForItem returns the open interactive rows for workItem, ordered
// by start.
func (o OpenRuns) InteractiveForItem(workItem string) []Record {
	var out []Record
	for _, r := range o.rows {
		if r.Mode == ModeInteractive && workItem != "" && r.WorkItem == workItem {
			out = append(out, r)
		}
	}
	return out
}

// Interactive returns the open interactive rows, ordered by start.
func (o OpenRuns) Interactive() []Record {
	var out []Record
	for _, r := range o.rows {
		if r.Mode == ModeInteractive {
			out = append(out, r)
		}
	}
	return out
}

// ByRunID returns the open row for runID.
func (o OpenRuns) ByRunID(runID string) (Record, bool) {
	if i := indexOf(o.rows, runID); i >= 0 {
		return o.rows[i], true
	}
	return Record{}, false
}

func indexOf(recs []Record, runID string) int {
	for i := range recs {
		if recs[i].ID == runID {
			return i
		}
	}
	return -1
}

func (s *Store) notify(action string) {
	if OnCommit != nil {
		OnCommit(s.wipnoteDir, s.RelPath(), action)
	}
}
