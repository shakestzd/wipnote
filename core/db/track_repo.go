package db

import (
	"database/sql"
	"fmt"
	"time"
)

// Track is a lightweight row struct for the tracks table.
// The full Node model lives in internal/models; this is for DB CRUD only.
type Track struct {
	ID          string
	Type        string
	Title       string
	Description string
	Priority    string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
}

// UpsertTrack inserts or updates a track row.
// On conflict by id, all mutable fields are updated.
// Tracks must be upserted BEFORE features to satisfy the FK constraint
// features.track_id → tracks.id.
func UpsertTrack(database *sql.DB, t *Track) error {
	typ := t.Type
	if typ == "" {
		typ = "track"
	}

	var completedAt sql.NullString
	if !t.CompletedAt.IsZero() {
		completedAt = sql.NullString{
			String: t.CompletedAt.UTC().Format(time.RFC3339),
			Valid:  true,
		}
	}

	_, err := database.Exec(`
		INSERT INTO tracks (id, type, title, description, priority, status,
			created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			title        = excluded.title,
			description  = excluded.description,
			priority     = excluded.priority,
			status       = excluded.status,
			updated_at   = excluded.updated_at,
			completed_at = excluded.completed_at`,
		t.ID, typ, t.Title, nullStr(t.Description),
		nullStr(t.Priority), t.Status,
		t.CreatedAt.UTC().Format(time.RFC3339),
		t.UpdatedAt.UTC().Format(time.RFC3339),
		completedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert track %s: %w", t.ID, err)
	}
	return nil
}
