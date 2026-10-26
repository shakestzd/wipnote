package sessionledger

import "github.com/shakestzd/wipnote/core/filelock"

// HarnessRetag describes one proposed change to a row's harness tag, whether
// it would be applied, and why. Like Correction, the dry-run and apply paths
// return the same decision so a repair pass reports what it would actually do.
type HarnessRetag struct {
	SessionID  string
	Old        string
	New        string
	Applied    bool
	WouldApply bool
	Reason     string
}

// Changed reports whether the retag alters the recorded tag.
func (r HarnessRetag) Changed() bool { return r.Old != r.New }

// RepairedHarness is the tag a row should carry given its recorded tag and the
// harness inferred from the session's canonical record (bug-5333ea49):
//
//   - a real harness is kept, whatever the inference says;
//   - an older spelling of one ("claude-code") is renamed to the current tag;
//   - anything else (the "human" agent fallback, an unknown value, empty)
//     takes the inferred harness, or "" (unknown) when none can be inferred.
func RepairedHarness(recorded, inferred string) string {
	if IsHarness(recorded) {
		return recorded
	}
	if h := HarnessFromAgent(recorded); h != "" {
		return h
	}
	return HarnessFromAgent(inferred)
}

// retagReason explains a harness change for operator-facing output.
func retagReason(old, inferred, next string) string {
	switch {
	case HarnessFromAgent(old) == next:
		return "renamed an older spelling of the harness"
	case next != "" && next == HarnessFromAgent(inferred):
		return "inferred from the session record's data-agent"
	}
	return "names no harness and none can be inferred; recorded as unknown"
}

// RetagHarness rewrites a row's harness tag to RepairedHarness(recorded,
// inferred), under the ledger lock, and never touches a row that already names
// a real harness. dryRun computes and returns the decision without writing.
// Returns ErrNoRow when the ledger has no row for the session.
func (s *Store) RetagHarness(sessionID, inferred string, dryRun bool) (HarnessRetag, error) {
	release := filelock.Guard(s.path)
	defer release()

	records, err := s.readLocked()
	if err != nil {
		return HarnessRetag{}, err
	}
	_, idx := findRecord(records, sessionID)
	if idx < 0 {
		return HarnessRetag{}, ErrNoRow
	}
	old := records[idx].Harness
	r := HarnessRetag{SessionID: sessionID, Old: old, New: RepairedHarness(old, inferred)}
	if !r.Changed() {
		r.Reason = "already correct"
		return r, nil
	}
	r.Reason = retagReason(old, inferred, r.New)
	if dryRun {
		r.WouldApply = true
		return r, nil
	}
	records[idx].Harness = r.New
	if err := writeAllLocked(s.path, records); err != nil {
		return HarnessRetag{}, err
	}
	s.notify("session harness repair")
	r.Applied = true
	return r, nil
}
