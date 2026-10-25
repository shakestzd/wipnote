package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// TestSessionEnd_DoesNotFinalizeHTML asserts that SessionEnd no longer calls
// FinalizeSessionHTML (that step is now owned exclusively by Stop). We verify by
// creating a sentinel session HTML and confirming SessionEnd leaves it unchanged.
// Stop is responsible for updating the event-count badge; SessionEnd must not.
func TestSessionEnd_DoesNotFinalizeHTML(t *testing.T) {
	td := setupTestDB(t)
	database := td.DB
	projectDir := t.TempDir()

	sessionID := "slim-no-html-003"
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_AGENT_ID", "claude-code")
	t.Setenv("WIPNOTE_AGENT_TYPE", "")
	t.Setenv("CLAUDE_ENV_FILE", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	if err := db.UpsertSession(database, &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Place a sentinel session HTML in the expected location.
	// FinalizeSessionHTML writes to .wipnote/sessions/<id>/session.html;
	// if SessionEnd calls it, this file would be overwritten.
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	const sentinel = `<html><body>SENTINEL-DO-NOT-OVERWRITE</body></html>`
	htmlPath := filepath.Join(sessDir, "session.html")
	if err := os.WriteFile(htmlPath, []byte(sentinel), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	evt := &CloudEvent{SessionID: sessionID, CWD: projectDir}
	if _, err := SessionEnd(evt, projectDir); err != nil {
		t.Fatalf("SessionEnd: %v", err)
	}

	// Verify the sentinel HTML is unchanged — SessionEnd must NOT finalize it.
	got, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != sentinel {
		t.Errorf("SessionEnd modified the session HTML (FinalizeSessionHTML must NOT be called here);\ngot:  %q\nwant: %q", string(got), sentinel)
	}
}
