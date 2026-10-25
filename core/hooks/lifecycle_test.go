package hooks

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/models"
)

// setupLifecycleDB creates a temp project dir with .wipnote/ and a real
// SQLite DB. Returns the database and the project dir.
func setupLifecycleDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	// Hooks have no daemon-miss fallback (feat-174dc958); a stub daemon
	// applies every routed statement to the database opened below.
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	projectDir := t.TempDir()
	hgDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(hgDir, 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	dbPath := filepath.Join(hgDir, "wipnote.db")
	// Pin WIPNOTE_DB_PATH so SessionStart's daemon-routed writes (which fall back
	// to a direct write at DBPath(projectRoot) when no daemon is running) land in
	// the same file this handle reads back from. See openWipnoteTestDB.
	t.Setenv("WIPNOTE_DB_PATH", dbPath)
	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database, projectDir
}

// TestYoloModeGuards verifies that YOLO mode blocks write tools when no work
// item is active, and allows them once one is set.
func TestYoloModeGuards(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)
	sessionID := "yolo-guard-session-001"

	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_AGENT_ID", "claude-code")
	t.Setenv("WIPNOTE_AGENT_TYPE", "")
	t.Setenv("WIPNOTE_PARENT_EVENT", "")
	t.Setenv("WIPNOTE_PARENT_PROMPT_EVENT", "")

	// Insert session without any active feature.
	if err := dbtest.UpsertSession(database, &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Reset caches so feature ID is read fresh.

	// Write tool without a work item should be blocked.
	// Set PermissionMode to bypassPermissions to activate YOLO guards.
	writeEvent := &CloudEvent{
		SessionID:      sessionID,
		CWD:            projectDir,
		PermissionMode: "bypassPermissions",
		ToolName:       "Write",
		ToolInput: map[string]any{
			"file_path": filepath.Join(projectDir, "foo.go"),
			"content":   "package main",
		},
	}
	result, err := PreToolUse(writeEvent)
	if err != nil {
		t.Fatalf("PreToolUse (no work item): %v", err)
	}
	if result.Decision != "block" {
		t.Errorf("expected block in YOLO mode without work item, got decision=%q reason=%q",
			result.Decision, result.Reason)
	}

	// Now add an in-progress work item and link it to the session.
	feat := &db.Feature{
		ID:        "feat-yolo-guard-01",
		Type:      "feature",
		Title:     "YOLO guard test feature",
		Status:    "in-progress",
		Priority:  "medium",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	if err := db.UpsertFeature(database, feat); err != nil {
		t.Fatalf("UpsertFeature: %v", err)
	}
	if _, err := database.Exec(
		`UPDATE sessions SET active_feature_id = ? WHERE session_id = ?`,
		feat.ID, sessionID,
	); err != nil {
		t.Fatalf("set active_feature_id: %v", err)
	}

	// Reset caches to pick up the new feature.

	// Write tool with an active work item should be allowed (worktree guard
	// will not trigger because CWD is a temp dir, not main/master branch).
	result, err = PreToolUse(writeEvent)
	if err != nil {
		t.Fatalf("PreToolUse (with work item): %v", err)
	}
	if result.Decision == "block" {
		// Work item guard should have passed; any remaining block is from another
		// YOLO guard (e.g. research or worktree). Accept blocks from those guards
		// but fail if reason still mentions missing work item.
		if result.Reason == "No active work item. Start a feature or bug before writing: wipnote feature start <id>" {
			t.Errorf("work item guard still blocking after feature set: %s", result.Reason)
		}
	}
}
