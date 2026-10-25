package db_test

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
)

func TestUpsertTrack_Insert(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	track := &db.Track{
		ID:        "trk-test-001",
		Type:      "track",
		Title:     "Test Track",
		Priority:  "high",
		Status:    "todo",
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := db.UpsertTrack(database, track); err != nil {
		t.Fatalf("UpsertTrack insert: %v", err)
	}

	var title, status string
	err := database.QueryRow(
		`SELECT title, status FROM tracks WHERE id = ?`, "trk-test-001",
	).Scan(&title, &status)
	if err != nil {
		t.Fatalf("query after insert: %v", err)
	}
	if title != "Test Track" {
		t.Errorf("title: got %q, want %q", title, "Test Track")
	}
	if status != "todo" {
		t.Errorf("status: got %q, want %q", status, "todo")
	}
}

func TestUpsertTrack_Update(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)
	track := &db.Track{
		ID:        "trk-test-002",
		Title:     "Original Title",
		Priority:  "medium",
		Status:    "todo",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.UpsertTrack(database, track); err != nil {
		t.Fatalf("first UpsertTrack: %v", err)
	}

	// Update title and status.
	track.Title = "Updated Title"
	track.Status = "in-progress"
	track.UpdatedAt = now.Add(time.Minute)

	if err := db.UpsertTrack(database, track); err != nil {
		t.Fatalf("second UpsertTrack: %v", err)
	}

	var title, status string
	database.QueryRow(
		`SELECT title, status FROM tracks WHERE id = ?`, "trk-test-002",
	).Scan(&title, &status)

	if title != "Updated Title" {
		t.Errorf("title after update: got %q, want %q", title, "Updated Title")
	}
	if status != "in-progress" {
		t.Errorf("status after update: got %q, want %q", status, "in-progress")
	}

	// Exactly one row must exist (upsert, not duplicate insert).
	var count int
	database.QueryRow(`SELECT COUNT(*) FROM tracks WHERE id = ?`, "trk-test-002").Scan(&count)
	if count != 1 {
		t.Errorf("row count: got %d, want 1", count)
	}
}

func TestUpsertTrack_FeatureFK(t *testing.T) {
	database := openTestDB(t)

	now := time.Now().UTC().Truncate(time.Second)

	// Upsert the track first to satisfy the FK.
	track := &db.Track{
		ID:        "trk-fk-001",
		Title:     "FK Test Track",
		Priority:  "medium",
		Status:    "todo",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.UpsertTrack(database, track); err != nil {
		t.Fatalf("UpsertTrack: %v", err)
	}

	// Now upsert a feature that references the track — must not fail FK.
	feat := &db.Feature{
		ID:        "feat-fk-001",
		Type:      "feature",
		Title:     "FK Test Feature",
		Status:    "todo",
		Priority:  "medium",
		TrackID:   "trk-fk-001",
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := db.UpsertFeature(database, feat); err != nil {
		t.Fatalf("UpsertFeature with track_id: %v", err)
	}

	var trackID string
	database.QueryRow(
		`SELECT track_id FROM features WHERE id = ?`, "feat-fk-001",
	).Scan(&trackID)
	if trackID != "trk-fk-001" {
		t.Errorf("track_id: got %q, want %q", trackID, "trk-fk-001")
	}
}
