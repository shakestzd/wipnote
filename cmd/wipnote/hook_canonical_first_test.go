// Slice 7 canonical-first acceptance tests (plan-ae0c37b2, feat-33c26c74).
//
// These integration tests lock in the architectural contract that hooks
// return SUCCESS to the user even when the writable SQLite handle is
// unavailable (writer_unavailable): the canonical NDJSON written upstream is
// the authoritative copy, and reindex recovers any derived-index rows the
// write path missed without duplicating them.
package main

import (
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/models"
)

// TestReplayAfterFallback_IsIdempotent writes the same event twice through
// the synchronous DB path (simulating "fallback hit, then later reindex
// re-applies the same event") and asserts exactly one row exists. The
// guarantee comes from db.UpsertEvent's INSERT OR REPLACE on the event_id
// primary key; this test locks in that contract for slice 7.
func TestReplayAfterFallback_IsIdempotent(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wipnote.db")
	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	// Seed a session row so the FK on agent_events resolves.
	if _, err := database.Exec(`
		INSERT INTO sessions (session_id, agent_assigned, created_at, status)
		VALUES (?, ?, ?, ?)`,
		"sess-replay", "claude-code", time.Now().UTC().Format(time.RFC3339), "active",
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}

	ev := &models.AgentEvent{
		EventID:      "evt-replay-1",
		AgentID:      "claude-code",
		EventType:    models.EventToolCall,
		Timestamp:    time.Now().UTC(),
		ToolName:     "Test",
		InputSummary: "replay test",
		SessionID:    "sess-replay",
		Status:       "completed",
		Source:       "hook",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}

	// First write — simulates the fallback path running the op directly.
	if err := dbpkg.UpsertEvent(database, ev); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	// Second write — simulates reindex catching up from canonical NDJSON.
	if err := dbpkg.UpsertEvent(database, ev); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE event_id = ?`, ev.EventID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("idempotence: want 1 row, got %d", count)
	}
}
