package db

import (
	"path/filepath"
	"testing"
)

// TestEventTree_SessionFamilyGrouping verifies that GetSessionsByFamily returns
// all sessions belonging to a family.
func TestEventTree_SessionFamilyGrouping(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "wipnote.db")
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	// Insert two sessions sharing a family.
	_, err = database.Exec(`
		INSERT INTO sessions (session_id, agent_assigned, status, session_family_id)
		VALUES ('root-1', 'claude-code', 'active', 'fam-xyz'),
		       ('resumed-1', 'claude-code', 'completed', 'fam-xyz'),
		       ('unrelated', 'codex', 'active', 'fam-other')`)
	if err != nil {
		t.Fatalf("insert test sessions: %v", err)
	}

	members, err := GetSessionsByFamily(database, "fam-xyz")
	if err != nil {
		t.Fatalf("GetSessionsByFamily: %v", err)
	}

	if len(members) != 2 {
		t.Errorf("family fam-xyz has %d members, want 2", len(members))
	}

	// Ensure unrelated is not included.
	for _, m := range members {
		if m == "unrelated" {
			t.Errorf("unexpected session %q in family fam-xyz", m)
		}
	}
}
