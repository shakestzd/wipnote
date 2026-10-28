package sessionledger

import (
	"reflect"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/filelock"
)

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
