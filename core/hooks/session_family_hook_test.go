package hooks

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

// TestEventTree_SessionFamilyGrouping verifies that root + subagent sessions
// written by the SessionStart hook are linkable by session_family_id.
func TestEventTree_SessionFamilyGrouping(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	// Simulate a root session started by the launcher (family = new session ID).
	rootID := "root-session-family-test-001"
	familyID := rootID // root is its own family

	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", familyID)
	t.Setenv("WIPNOTE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")
	t.Setenv("CLAUDE_ENV_FILE", "")

	event := &CloudEvent{SessionID: rootID, CWD: projectDir}
	if _, err := SessionStart(event, database, projectDir); err != nil {
		t.Fatalf("SessionStart (root): %v", err)
	}

	// Simulate a resumed session in the same family.
	resumedID := "resumed-session-family-test-001"
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", familyID)
	event2 := &CloudEvent{SessionID: resumedID, CWD: projectDir}
	if _, err := SessionStart(event2, database, projectDir); err != nil {
		t.Fatalf("SessionStart (resumed): %v", err)
	}

	// The family index file records both sessions under the family.
	idx, err := agent.ReadSessionFamilyIndex(projectDir)
	if err != nil {
		t.Fatalf("ReadSessionFamilyIndex: %v", err)
	}
	if idx[rootID] != familyID {
		t.Errorf("family index: %q -> %q, want %q", rootID, idx[rootID], familyID)
	}
	if idx[resumedID] != familyID {
		t.Errorf("family index: %q -> %q, want %q", resumedID, idx[resumedID], familyID)
	}
}

// TestSessionStart_FamilyFallback verifies that when WIPNOTE_SESSION_FAMILY_ID
// is unset, the session gets its own ID as the family (self-as-family backfill).
func TestSessionStart_FamilyFallback(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	sessID := "solo-session-family-test-002"
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "") // explicitly unset
	t.Setenv("WIPNOTE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")
	t.Setenv("CLAUDE_ENV_FILE", "")

	event := &CloudEvent{SessionID: sessID, CWD: projectDir}
	if _, err := SessionStart(event, database, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	// The session should be its own family in the family index.
	idx, err := agent.ReadSessionFamilyIndex(projectDir)
	if err != nil {
		t.Fatalf("ReadSessionFamilyIndex: %v", err)
	}
	if idx[sessID] != sessID {
		t.Errorf("family index: %q -> %q, want itself", sessID, idx[sessID])
	}
}
