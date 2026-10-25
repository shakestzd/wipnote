package db_test

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
)

// TestSessionPromptLabel_StripsTags verifies that SessionPromptLabel returns
// sanitized text (tags removed, whitespace collapsed) rather than raw markup.
func TestSessionPromptLabel_StripsTags(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Seed last_user_query with a raw markup-laden string.
	rawLabel := "Some task\n<task-notification>\n<task-id>abc</task-id>\n</task-notification>"
	if _, err := database.Exec(
		`UPDATE sessions SET last_user_query = ?, last_user_query_at = ? WHERE session_id = ?`,
		rawLabel,
		time.Now().UTC().Format(time.RFC3339),
		"sess-test",
	); err != nil {
		t.Fatalf("seed last_user_query: %v", err)
	}

	got := db.SessionPromptLabel(database, "sess-test")
	if got == "" {
		t.Fatal("expected non-empty label")
	}
	// Tags must be removed and whitespace collapsed.
	want := "Some task"
	if got != want {
		t.Errorf("SessionPromptLabel = %q, want %q", got, want)
	}
}

// TestSessionPromptLabel_SkipsAllTagsCandidate verifies that when the
// top-ranked candidate is entirely tags (sanitizes to empty), the next
// non-empty sanitized candidate is returned instead.
func TestSessionPromptLabel_SkipsAllTagsCandidate(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Seed last_user_query with only tags so it sanitizes to "".
	// Seed a user message with a real prompt as the fallback.
	onlyTags := "<task-notification><task-id>abc</task-id></task-notification>"
	if _, err := database.Exec(
		`UPDATE sessions SET last_user_query = ?, last_user_query_at = ? WHERE session_id = ?`,
		onlyTags,
		time.Now().UTC().Add(-time.Second).Format(time.RFC3339),
		"sess-test",
	); err != nil {
		t.Fatalf("seed last_user_query (tags-only): %v", err)
	}

	// Insert a real user message with a timestamp slightly older.
	// The messages schema uses (session_id, ordinal, role, content, timestamp).
	if _, err := database.Exec(
		`INSERT INTO messages (session_id, ordinal, role, content, timestamp)
		 VALUES (?, ?, ?, ?, ?)`,
		"sess-test", 1, "user",
		"Fix the broken login flow",
		time.Now().UTC().Add(-2*time.Second).Format(time.RFC3339),
	); err != nil {
		t.Fatalf("seed message: %v", err)
	}

	got := db.SessionPromptLabel(database, "sess-test")
	want := "Fix the broken login flow"
	if got != want {
		t.Errorf("SessionPromptLabel = %q, want %q (should skip tags-only candidate)", got, want)
	}
}
