package db_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// setupTestDB opens an in-memory database with schema and a test session.
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbtest.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Insert a session so FK constraints pass.
	now := time.Now().UTC()
	sess := &models.Session{
		SessionID:     "sess-test",
		AgentAssigned: "claude-code",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := dbtest.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	return database
}

func TestUpsertEvent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:   "evt-upsert-1",
		AgentID:   "claude-code",
		EventType: models.EventToolCall,
		Timestamp: now,
		ToolName:  "Bash",
		SessionID: "sess-test",
		Status:    "started",
		Source:    "hook",
		CreatedAt: now,
		UpdatedAt: now,
	}

	// First insert.
	if err := db.UpsertEvent(database, ev); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Verify it exists.
	got, err := dbtest.GetEvent(database, "evt-upsert-1")
	if err != nil {
		t.Fatalf("GetEvent after first upsert: %v", err)
	}
	if got.Status != "started" {
		t.Errorf("status: got %q, want %q", got.Status, "started")
	}

	// Upsert with updated status (should replace).
	ev.Status = "completed"
	ev.OutputSummary = "done"
	if err := db.UpsertEvent(database, ev); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err = dbtest.GetEvent(database, "evt-upsert-1")
	if err != nil {
		t.Fatalf("GetEvent after second upsert: %v", err)
	}
	if got.Status != "completed" {
		t.Errorf("status after upsert: got %q, want %q", got.Status, "completed")
	}
	if got.OutputSummary != "done" {
		t.Errorf("output_summary: got %q, want %q", got.OutputSummary, "done")
	}
}
