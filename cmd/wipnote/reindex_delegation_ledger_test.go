package main

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/delegationledger"
)

// TestReindex_DelegationLedgerRows pins that the derived index holds exactly
// the canonical rows, every field included, and that a later Close is picked
// up by the next pass rather than leaving the stale open row behind.
func TestReindex_DelegationLedgerRows(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	store := delegationledger.NewStore(wipnoteDir)
	t0 := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	exit := 2
	open := delegationledger.Record{
		ID: "dlg-0123456789ab", Mode: delegationledger.ModeInteractive, Harness: "claude",
		Name: "parser", Label: "review", WorkItem: "feat-dd8b0cde", Link: "sid",
		CallerSession: "caller-1", WorkerSessions: []string{"s-2", "s-1"}, Host: "host",
		Started: t0, ResumedFrom: "dlg-ba9876543210",
	}
	oneshot := delegationledger.Record{
		ID: "dlg-aaaaaaaaaaaa", Mode: delegationledger.ModeOneshot, Harness: "codex-review",
		Host: "container", Started: t0.Add(time.Minute), Ended: t0.Add(2 * time.Minute),
		Exit: &exit, EndReason: delegationledger.EndExit, UsageInput: 10, UsageCached: 4, UsageOutput: 3,
	}
	for _, r := range []delegationledger.Record{open, oneshot} {
		if _, err := store.Open(r); err != nil {
			t.Fatalf("Open %s: %v", r.ID, err)
		}
	}

	database, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		t.Fatalf("OpenEphemeralProjection: %v", err)
	}
	defer database.Close()

	assertIndexMatchesLedger(t, database, store, 2)
	if err := store.Close(open.ID, delegationledger.CloseInfo{Ended: t0.Add(time.Hour), Reason: delegationledger.EndStop}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertIndexMatchesLedger(t, database, store, 2)

	// The pass is wired into the rebuild every command and `wipnote reindex`
	// run, not only callable on its own.
	fresh, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		t.Fatalf("OpenEphemeralProjection: %v", err)
	}
	defer fresh.Close()
	if err := hydrateCompatibilityDB(fresh, wipnoteDir); err != nil {
		t.Fatalf("hydrateCompatibilityDB: %v", err)
	}
	want, _ := store.ReadAll()
	if got, err := dbpkg.ListDelegationRuns(fresh); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("hydrated index = %+v (%v), want %+v", got, err, want)
	}
}

func assertIndexMatchesLedger(t *testing.T, database *sql.DB, store *delegationledger.Store, wantRows int) {
	t.Helper()
	inserted, errs := reindexDelegationRuns(database, filepath.Dir(store.Path()), false)
	if errs != 0 || inserted != wantRows {
		t.Fatalf("reindexDelegationRuns = (%d, %d errors), want (%d, 0)", inserted, errs, wantRows)
	}
	want, err := store.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	got, err := dbpkg.ListDelegationRuns(database)
	if err != nil {
		t.Fatalf("ListDelegationRuns: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("index rows differ from the ledger:\n got %+v\nwant %+v", got, want)
	}
}
