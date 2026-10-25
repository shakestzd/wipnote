package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/shakestzd/wipnote/core/db"
)

// openIsolatedDB opens a file-based SQLite in a temp dir so tests
// don't interfere with the shared in-memory database used by other suites.
func openIsolatedDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	database, err := dbtest.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestSetAndGetActiveWorkItem(t *testing.T) {
	database := openIsolatedDB(t)

	// Empty table returns "".
	got := dbtest.ActiveWorkItem(database, "sess-1", "agent-1")
	if got != "" {
		t.Fatalf("want empty, got %q", got)
	}

	// Set a claim and read it back.
	if err := dbtest.SetActiveWorkItem(database, "sess-1", "agent-1", "feat-abc"); err != nil {
		t.Fatalf("SetActiveWorkItem: %v", err)
	}
	got = dbtest.ActiveWorkItem(database, "sess-1", "agent-1")
	if got != "feat-abc" {
		t.Fatalf("want feat-abc, got %q", got)
	}
}

func TestSetActiveWorkItem_Overwrite(t *testing.T) {
	database := openIsolatedDB(t)

	if err := dbtest.SetActiveWorkItem(database, "sess-1", "agent-1", "feat-old"); err != nil {
		t.Fatalf("first set: %v", err)
	}
	// Overwrite with a different item — must not error.
	if err := dbtest.SetActiveWorkItem(database, "sess-1", "agent-1", "feat-new"); err != nil {
		t.Fatalf("overwrite set: %v", err)
	}
	got := dbtest.ActiveWorkItem(database, "sess-1", "agent-1")
	if got != "feat-new" {
		t.Fatalf("want feat-new, got %q", got)
	}
}

func TestNormaliseAgentID(t *testing.T) {
	if got := db.NormaliseAgentID(""); got != db.AgentRootSentinel {
		t.Errorf("empty: want %q, got %q", db.AgentRootSentinel, got)
	}
	if got := db.NormaliseAgentID("a1b2"); got != "a1b2" {
		t.Errorf("non-empty: want a1b2, got %q", got)
	}
}
