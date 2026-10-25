package db

import (
	"testing"
)

// TestGateRecordCountsForWorkItem covers the outcome-evidence query
// compliance auto (feat-f9118b9c) joins against the spec/diff comparison: a
// pass/fail tally across every session that ran a gate for the item, an
// unrelated item never leaking in, and a genuine (0,0) for an item with no
// gate history at all — which callers must read as "no evidence", never as
// "passed".
func TestGateRecordCountsForWorkItem(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	mustInsert := func(rec *GateRecord) {
		t.Helper()
		if _, err := InsertGateRecordIfAbsent(database, rec); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Two passes and one fail for feat-x, spread across sessions.
	mustInsert(&GateRecord{SessionID: "sess-1", WorkItemID: "feat-x", ProjectType: "go", GateCommand: "go build ./...", Status: "pass", Source: "check"})
	mustInsert(&GateRecord{SessionID: "sess-2", WorkItemID: "feat-x", ProjectType: "go", GateCommand: "go build ./...", Status: "pass", Source: "check"})
	mustInsert(&GateRecord{SessionID: "sess-3", WorkItemID: "feat-x", ProjectType: "go", GateCommand: "go build ./...", Status: "fail", Source: "check"})
	// A different item's records must not leak into feat-x's tally.
	mustInsert(&GateRecord{SessionID: "sess-4", WorkItemID: "feat-y", ProjectType: "go", GateCommand: "go build ./...", Status: "fail", Source: "check"})

	pass, fail, err := GateRecordCountsForWorkItem(database, "feat-x")
	if err != nil {
		t.Fatalf("counts: %v", err)
	}
	if pass != 2 || fail != 1 {
		t.Errorf("feat-x: got pass=%d fail=%d, want pass=2 fail=1", pass, fail)
	}

	// An item with no gate history at all: a genuine, complete zero — not a
	// sampled ratio and not a passing signal.
	pass, fail, err = GateRecordCountsForWorkItem(database, "feat-never-gated")
	if err != nil {
		t.Fatalf("counts (no history): %v", err)
	}
	if pass != 0 || fail != 0 {
		t.Errorf("feat-never-gated: got pass=%d fail=%d, want 0,0", pass, fail)
	}

	// Nil db and empty work item id are defensive no-ops, not errors.
	if pass, fail, err := GateRecordCountsForWorkItem(nil, "feat-x"); err != nil || pass != 0 || fail != 0 {
		t.Errorf("nil db: got pass=%d fail=%d err=%v", pass, fail, err)
	}
	if pass, fail, err := GateRecordCountsForWorkItem(database, ""); err != nil || pass != 0 || fail != 0 {
		t.Errorf("empty work item id: got pass=%d fail=%d err=%v", pass, fail, err)
	}
}
