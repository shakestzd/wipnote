package sessionledger

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/filelock"
)

// FileName is the git-tracked canonical ledger, a sibling of
// .wipnote/architecture.html and named to sit beside — not inside — the
// gitignored .wipnote/sessions/ telemetry tree it outlives.
const FileName = "sessions-ledger.html"

// ErrNoRow is returned by Close when the ledger holds no row for the session.
// Callers treat it as benign: it means the session started before the ledger
// existed, or its start row was never written, not that anything failed.
var ErrNoRow = errors.New("sessionledger: no row for session")

// OnCommit, when non-nil, is invoked after a successful ledger mutation with
// the wipnote dir, the repo-relative path of the file that changed, and the
// transition verb. cmd/wipnote sets it to the commit-queue producer so ledger
// writes get committed like every other canonical artifact.
//
// It is a package-level seam (the same shape as claimledger.OnCommit) because
// core/ may not import internal/commitqueue, while every writer — CLI, hook
// handler, and retention sweep alike — runs inside the wipnote binary that does.
var OnCommit func(wipnoteDir, relPath, action string)

// Store reads and writes the sessions ledger for one project.
type Store struct {
	wipnoteDir string
	path       string
}

// NewStore returns a Store for the ledger under wipnoteDir. The file is created
// lazily on first write — a read-only consumer must not leave an empty ledger
// behind.
func NewStore(wipnoteDir string) *Store {
	return &Store{wipnoteDir: wipnoteDir, path: filepath.Join(wipnoteDir, FileName)}
}

// Path returns the absolute ledger path.
func (s *Store) Path() string { return s.path }

// RelPath returns the ledger path relative to the repo root (the parent of
// .wipnote), for git staging and commit intents. Absolute host paths must never
// leak into wipnote descriptions or commit messages, so callers use this.
func (s *Store) RelPath() string {
	repoRoot := filepath.Dir(s.wipnoteDir)
	if rel, err := filepath.Rel(repoRoot, s.path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(s.wipnoteDir), FileName))
}

// ReadAll returns every recorded session, ordered by start. A ledger that does
// not exist yet reads as empty rather than as an error.
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

// Get returns the row for one session.
func (s *Store) Get(sessionID string) (Record, bool, error) {
	recs, err := s.ReadAll()
	if err != nil {
		return Record{}, false, err
	}
	for _, r := range recs {
		if r.SessionID == sessionID {
			return r, true, nil
		}
	}
	return Record{}, false, nil
}

// Open records the START of a session run.
//
// The first Open for an id appends the session's row: run 1. An Open on a row
// whose latest run has ENDED is a resume (`--resume`, `--continue`, a Codex
// resume — the id is reused): it appends one run-start event line (runs.go)
// with the incoming start, so a resumed-then-exited session records its second
// end instead of staying closed at its first (bug-2a96e9cb). An Open on a row
// whose latest run is OPEN is a replayed hook and writes nothing, which keeps
// the ledger one-row-per-session. Both writes are constant-time tail appends.
//
// Only the start signal itself may call Open: the SessionStart hook, or a
// command minting a new session. A late writer that merely sees a session id
// in its environment uses Ensure, which never resumes — a leftover process
// running a CLI command must not reopen the session it outlived.
//
// A harness that is the agent fallback identity is refused with
// ErrAgentHarness rather than recorded. On an existing row, Open corrects a
// harness that is empty or names no harness (see correctHarnessLocked), so the
// authoritative SessionStart write wins over an earlier late writer's guess
// (bug-5333ea49).
//
// Returns whether a new row or run was written.
func (s *Store) Open(r Record) (bool, error) { return s.open(r, true) }

// Ensure records a session's row when none exists, and otherwise only corrects
// a guessed harness. It never starts a run: it is the late-writer path
// (EnsureSession), whose caller cannot tell a live session from one it outlived.
func (s *Store) Ensure(r Record) (bool, error) { return s.open(r, false) }

func (s *Store) open(r Record, mayResume bool) (bool, error) {
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now().UTC()
	}
	r.EndedAt, r.EndSource, r.Runs = time.Time{}, EndSourceUnknown, nil
	r.ArchivePath = ""
	r.Events = 0
	if err := r.Validate(); err != nil {
		return false, err
	}
	if isAgentFallback(r.Harness) {
		return false, ErrAgentHarness
	}
	if containerwrite.Active() {
		return s.openOwn(r)
	}

	release := filelock.Guard(s.path)
	defer release()

	existing, err := s.readLocked()
	if err != nil {
		return false, err
	}
	if _, found := findRecord(existing, r.SessionID); found >= 0 {
		resumed := false
		if mayResume && !existing[found].IsOpen() {
			if resumed, err = s.resumeLocked(&existing[found], r.StartedAt); err != nil {
				return false, err
			}
		}
		// The one thing a start may still fix is a harness a late writer guessed.
		return resumed, s.correctHarnessLocked(existing, found, r.Harness)
	}

	if err := appendRowLocked(s.path, r); err != nil {
		return false, err
	}
	s.notify("session start")
	return true, nil
}

// correctHarnessLocked replaces the harness on records[idx] when the row
// carries none, or carries a value that names no harness (an agent identity
// such as "human" or "claude-code" from an older late writer), and harness is
// a real one. A row that already names a harness is never retagged: the first
// real harness recorded is the true one (bug-5333ea49). Caller holds the lock.
func (s *Store) correctHarnessLocked(records []Record, idx int, harness string) error {
	if !IsHarness(harness) || IsHarness(records[idx].Harness) {
		return nil
	}
	records[idx].Harness = harness
	if err := writeAllLocked(s.path, records); err != nil {
		return err
	}
	s.notify("session harness")
	return nil
}

// Close records the END of a session's latest run.
//
// Run 1's end is stamped in place on the session row, so one session stays one
// row and an older reader sees it. It is rarer than Open by construction (a
// session is opened before it can be closed) and never fires more than once
// per session end, so paying the serialize cost here keeps it off the frequent
// path. A resumed run's end is a run-end event line appended at the tail.
//
// A row whose latest run is already closed is left untouched and reported as
// no change: the first recorded end is the true one, and a later reconcile pass
// must not move it. Returns ErrNoRow when the session predates the ledger.
func (s *Store) Close(sessionID string, endedAt time.Time) error {
	if endedAt.IsZero() {
		endedAt = time.Now().UTC()
	}
	if containerwrite.Active() {
		return containerwrite.CloseSession(s.wipnoteDir, sessionID, endedAt)
	}

	release := filelock.Guard(s.path)
	defer release()

	records, err := s.readLocked()
	if err != nil {
		return err
	}
	_, idx := findRecord(records, sessionID)
	if idx < 0 {
		return ErrNoRow
	}
	rec := records[idx]
	if !rec.IsOpen() {
		return nil
	}
	if start := rec.LatestRun().StartedAt; endedAt.Before(start) {
		// On a resumed run an end before its start belongs to an earlier
		// run (a late SessionEnd catch-up); closing the live run with it
		// would turn its processes into orphans. Run 1 has no earlier run,
		// so there it is clock skew and clamps.
		if rec.resumed() {
			return nil
		}
		endedAt = start
	}
	if rec.resumed() {
		ev := runEvent{kind: runEventEnd, at: endedAt, src: EndSourceLiveClose}
		err = ledgerFormat.AppendRow(s.path, marshalRunEvent(sessionID, ev))
	} else {
		records[idx].endLatest(endedAt, EndSourceLiveClose)
		err = writeAllLocked(s.path, records)
	}
	if err != nil {
		return err
	}
	s.notify("session end")
	return nil
}

// Enrichment is what a later pass learns about a session it did not start.
// Zero fields mean "nothing new"; Enrich never clears a value that is already
// recorded, so a backfill can run repeatedly without degrading a richer row.
type Enrichment struct {
	Harness    string
	ProjectDir string
	StartedAt  time.Time
	EndedAt    time.Time
	// EndSource names where EndedAt came from. It is recorded alongside the end
	// so a later Correct can tell a tarball mtime from a real close without
	// guessing; leaving it unset marks the end as unattributed, which ranks
	// lowest and invites re-derivation.
	EndSource   EndSource
	ArchivePath string
	Events      int
}

// Enrich fills in what a later pass learned about a session, and CREATES the
// row when none exists.
//
// The create case is the backfill path and the reason this is not just an
// update: a session archived before the ledger existed has no start row, but the
// archive proves it existed and carries its end time. Writing the row is what
// turns that proof into a resolvable edge target. A row created here records
// only what the source actually knew — the start time falls back to the end
// time rather than being invented, since a row with a fabricated start would
// misreport the interval to every consumer that joins on it.
//
// Returns whether the ledger changed.
func (s *Store) Enrich(sessionID string, e Enrichment) (bool, error) {
	if isAgentFallback(e.Harness) {
		e.Harness = "" // an agent identity is not a harness: record it as unknown
	}
	if containerwrite.Active() {
		return false, nil // the host enriches the folded row
	}
	release := filelock.Guard(s.path)
	defer release()

	records, err := s.readLocked()
	if err != nil {
		return false, err
	}

	_, idx := findRecord(records, sessionID)
	if idx < 0 {
		return s.createFromEnrichmentLocked(sessionID, e)
	}

	before := records[idx]
	merged := mergeEnrichment(before, e)
	if reflect.DeepEqual(merged, before) {
		return false, nil
	}

	records[idx] = merged
	if err := writeAllLocked(s.path, records); err != nil {
		return false, err
	}
	s.notify("session record")
	return true, nil
}

// createFromEnrichmentLocked appends the row a backfill proves existed. The
// start falls back to the end rather than being invented. Caller holds the lock.
func (s *Store) createFromEnrichmentLocked(sessionID string, e Enrichment) (bool, error) {
	started := e.StartedAt
	if started.IsZero() {
		started = e.EndedAt
	}
	created := Record{
		SessionID:   sessionID,
		Harness:     e.Harness,
		ProjectDir:  e.ProjectDir,
		StartedAt:   started,
		EndedAt:     e.EndedAt,
		ArchivePath: e.ArchivePath,
		Events:      e.Events,
	}
	if !created.IsOpen() {
		created.EndSource = e.EndSource
	}
	if err := created.Validate(); err != nil {
		return false, err
	}
	if err := appendRowLocked(s.path, created); err != nil {
		return false, err
	}
	s.notify("session record")
	return true, nil
}

// mergeEnrichment fills before's unknown fields from e. An end lands on the
// latest run only while it is open, and never when it precedes that run's
// start: such an end describes an earlier run (a backfill from the first run's
// events), and must not close the run a resume opened.
func mergeEnrichment(before Record, e Enrichment) Record {
	merged := before
	if merged.Harness == "" {
		merged.Harness = e.Harness
	}
	if merged.ProjectDir == "" {
		merged.ProjectDir = e.ProjectDir
	}
	if merged.StartedAt.IsZero() && !e.StartedAt.IsZero() {
		merged.StartedAt = e.StartedAt
	}
	if merged.IsOpen() && !e.EndedAt.IsZero() {
		end, start := e.EndedAt, merged.LatestRun().StartedAt
		switch {
		case !end.Before(start):
			merged.endLatest(end, e.EndSource)
		case !merged.resumed():
			merged.endLatest(start, e.EndSource)
		}
	}
	if merged.ArchivePath == "" {
		merged.ArchivePath = e.ArchivePath
	}
	if merged.Events == 0 && e.Events > 0 {
		merged.Events = e.Events
	}
	return merged
}

// Correction describes one proposed change to a row's end, whether it would be
// applied, and why. It is returned by both the dry-run and the apply path so a
// caller can report the same decision it would make.
type Correction struct {
	SessionID string

	OldEnd    time.Time
	OldSource EndSource
	NewEnd    time.Time
	NewSource EndSource

	// Applied is true when the change was written. A correction that is refused
	// (weaker source) or redundant (same value) is still returned, with Applied
	// false and Reason saying which, so a repair pass can explain its no-ops
	// rather than silently skipping them.
	Applied bool
	// WouldApply is true when a dry-run passed every guard and only the dry-run
	// flag stopped the write. It exists so a caller can report the decision this
	// function made instead of re-deriving it — a duplicated rule is a rule that
	// can drift out of agreement with the one that actually governs writes.
	WouldApply bool
	Reason     string
}

// Changed reports whether the correction alters the recorded end VALUE.
// Gaining provenance for an end that was already correct is not a change to the
// session's history, and a repair pass reports the two separately.
func (c Correction) Changed() bool { return !c.NewEnd.Equal(c.OldEnd) }

// AttributionOnly reports whether the only difference is provenance: the end
// was already right, but the row could not say where it came from.
func (c Correction) AttributionOnly() bool {
	return !c.Changed() && c.NewSource != c.OldSource
}

// Correct replaces a row's end when — and only when — the incoming source is
// more trustworthy than the one already recorded. On a resumed session it is
// the LATEST run's end that is corrected.
//
// This is the deliberate counterpart to Enrich, not a loosening of it. Enrich
// fills gaps and never moves a value that is already there, which is right for
// a reconcile pass that must not disturb a real close. But a value recorded
// from a weak source is not a fact to protect, and with no way to correct it a
// canonical artifact has no repair path at all — the gap this exists to close.
//
// The trust ordering (EndSource.Rank) is the whole rule. There is no threshold
// and no inference from the values themselves, because the one value-based rule
// that looks general — "an end after the last recorded event is wrong" — is
// false whenever SessionEnd fires after the last tool event, which is normal.
//
// dryRun computes and returns the decision without writing, so the caller can
// show exactly what would change.
func (s *Store) Correct(sessionID string, end time.Time, src EndSource, dryRun bool) (Correction, error) {
	if containerwrite.Active() {
		return Correction{}, ErrContainerWorker
	}
	release := filelock.Guard(s.path)
	defer release()

	records, err := s.readLocked()
	if err != nil {
		return Correction{}, err
	}
	_, idx := findRecord(records, sessionID)
	if idx < 0 {
		return Correction{}, ErrNoRow
	}

	before := records[idx]
	c := Correction{
		SessionID: sessionID,
		OldEnd:    before.EndedAt,
		OldSource: before.EndSource,
		NewEnd:    end,
		NewSource: src,
	}

	switch {
	case end.IsZero():
		c.Reason = "no end available from any source"
		return c, nil
	case end.Before(before.LatestRun().StartedAt):
		// Never record an end before the (latest run's) start; that is corrupt
		// whatever its provenance says.
		c.Reason = "candidate end precedes the recorded start"
		return c, nil
	case !src.OutranksRecorded(before.EndSource):
		c.Reason = "recorded end came from " + describeSource(before.EndSource) +
			", which is at least as trustworthy as " + describeSource(src)
		return c, nil
	case !c.Changed() && !c.AttributionOnly():
		c.Reason = "already correct"
		return c, nil
	}

	if dryRun {
		c.WouldApply = true
		if c.AttributionOnly() {
			c.Reason = "would record this end as " + describeSource(src) + "; the value is already correct"
		} else {
			c.Reason = "would replace " + describeSource(before.EndSource) + " with " + describeSource(src)
		}
		return c, nil
	}

	attributionOnly := c.AttributionOnly()
	records[idx].endLatest(end, src)
	if err := writeAllLocked(s.path, records); err != nil {
		return Correction{}, err
	}
	s.notify("session repair")
	c.Applied = true
	if attributionOnly {
		c.Reason = "recorded this end as " + describeSource(src) + "; the value was already correct"
	} else {
		c.Reason = "replaced " + describeSource(before.EndSource) + " with " + describeSource(src)
	}
	return c, nil
}

// describeSource renders a source for operator-facing output, naming the
// unattributed case explicitly rather than printing an empty string.
func describeSource(s EndSource) string {
	if s == EndSourceUnknown {
		return "an unattributed end"
	}
	return string(s)
}

// readLocked reads the ledger while the caller holds the write guard.
func (s *Store) readLocked() ([]Record, error) {
	recs, err := ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return recs, nil
}

// findRecord returns the record for sessionID and its index, or index -1.
func findRecord(recs []Record, sessionID string) (Record, int) {
	for i := range recs {
		if recs[i].SessionID == sessionID {
			return recs[i], i
		}
	}
	return Record{}, -1
}

func (s *Store) notify(action string) {
	if OnCommit == nil {
		return
	}
	OnCommit(s.wipnoteDir, s.RelPath(), action)
}
