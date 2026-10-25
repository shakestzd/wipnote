package db_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/models"
)

func insertTestFeatures(t *testing.T, database *sql.DB, ids ...string) {
	t.Helper()
	for _, id := range ids {
		database.Exec(`INSERT OR IGNORE INTO features (id, type, title, status) VALUES (?, 'feature', 'test', 'todo')`, id)
	}
}

func TestGetCommitsBySession(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	commits := []models.GitCommit{
		{
			CommitHash: "abc1234",
			SessionID:  "sess-test",
			FeatureID:  "feat-aaa",
			Message:    "feat: first commit",
			Timestamp:  now,
		},
		{
			CommitHash: "def5678",
			SessionID:  "sess-test",
			FeatureID:  "feat-bbb",
			Message:    "feat: second commit",
			Timestamp:  now.Add(time.Second),
		},
		{
			CommitHash: "ghi9012",
			SessionID:  "sess-other",
			FeatureID:  "feat-ccc",
			Message:    "feat: other session commit",
			Timestamp:  now.Add(2 * time.Second),
		},
	}

	for i := range commits {
		if err := dbtest.InsertGitCommit(database, &commits[i]); err != nil {
			t.Fatalf("InsertGitCommit %s: %v", commits[i].CommitHash, err)
		}
	}

	results, err := db.GetCommitsBySession(database, "sess-test")
	if err != nil {
		t.Fatalf("GetCommitsBySession: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 commits for sess-test, got %d", len(results))
	}

	// Should be ordered DESC by timestamp.
	if results[0].CommitHash != "def5678" {
		t.Errorf("first result: got %q, want %q", results[0].CommitHash, "def5678")
	}
	if results[1].CommitHash != "abc1234" {
		t.Errorf("second result: got %q, want %q", results[1].CommitHash, "abc1234")
	}
	if results[0].Message != "feat: second commit" {
		t.Errorf("message: got %q, want %q", results[0].Message, "feat: second commit")
	}

	// Unknown session returns empty slice (not error).
	results2, err := db.GetCommitsBySession(database, "sess-unknown")
	if err != nil {
		t.Fatalf("GetCommitsBySession (unknown): %v", err)
	}
	if len(results2) != 0 {
		t.Errorf("expected empty slice for unknown session, got %d commits", len(results2))
	}
}

func TestTraceCommit(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	// Insert a feature with a track.
	database.Exec(`INSERT INTO tracks (id, title, status, created_at, updated_at) VALUES ('trk-trace', 'Trace Track', 'todo', datetime('now'), datetime('now'))`)
	database.Exec(`INSERT INTO features (id, type, title, status, priority, track_id, created_at, updated_at) VALUES ('feat-trace', 'feature', 'Trace Feature', 'done', 'medium', 'trk-trace', datetime('now'), datetime('now'))`)

	// Insert a commit linked to that feature.
	commit := models.GitCommit{
		CommitHash: "abc1234567890",
		SessionID:  "sess-test",
		FeatureID:  "feat-trace",
		Message:    "feat: traced commit",
		Timestamp:  time.Now().UTC(),
	}
	if err := dbtest.InsertGitCommit(database, &commit); err != nil {
		t.Fatalf("InsertGitCommit: %v", err)
	}

	// Full SHA match.
	results, err := db.TraceCommit(database, "abc1234567890")
	if err != nil {
		t.Fatalf("TraceCommit full: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FeatureID != "feat-trace" {
		t.Errorf("feature_id: got %q, want feat-trace", results[0].FeatureID)
	}
	if results[0].TrackID != "trk-trace" {
		t.Errorf("track_id: got %q, want trk-trace", results[0].TrackID)
	}
	if results[0].SessionID != "sess-test" {
		t.Errorf("session_id: got %q, want sess-test", results[0].SessionID)
	}

	// Prefix match.
	results2, err := db.TraceCommit(database, "abc1234")
	if err != nil {
		t.Fatalf("TraceCommit prefix: %v", err)
	}
	if len(results2) != 1 {
		t.Errorf("prefix match: expected 1 result, got %d", len(results2))
	}

	// No match.
	results3, err := db.TraceCommit(database, "zzz0000")
	if err != nil {
		t.Fatalf("TraceCommit no match: %v", err)
	}
	if len(results3) != 0 {
		t.Errorf("expected 0 results for unknown SHA, got %d", len(results3))
	}
}
