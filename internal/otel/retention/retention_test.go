package retention_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/otel/retention"
	_ "modernc.org/sqlite"
)

// openTestDB creates an in-memory SQLite DB with the minimal sessions schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE sessions (
		session_id   TEXT PRIMARY KEY,
		status       TEXT NOT NULL DEFAULT 'active',
		completed_at TEXT
	)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// insertSession inserts a minimal session row.
func insertSession(t *testing.T, db *sql.DB, sessionID, status string, completedAt *time.Time) {
	t.Helper()
	var completedStr *string
	if completedAt != nil {
		s := completedAt.UTC().Format(time.RFC3339)
		completedStr = &s
	}
	_, err := db.Exec(`INSERT INTO sessions (session_id, status, completed_at) VALUES (?, ?, ?)`,
		sessionID, status, completedStr)
	if err != nil {
		t.Fatalf("insert session %s: %v", sessionID, err)
	}
}

// makeSessionDir creates a synthetic session dir with an events.ndjson file.
func makeSessionDir(t *testing.T, htmlgraphDir, sessionID, content string) {
	t.Helper()
	dir := filepath.Join(htmlgraphDir, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "events.ndjson"), []byte(content), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
	offsetStr := fmt.Sprintf("%d", len(content))
	if err := os.WriteFile(filepath.Join(dir, ".index-offset"), []byte(offsetStr), 0o644); err != nil {
		t.Fatalf("write index-offset: %v", err)
	}
}

func TestRun_ArchivesOldCompletedSession(t *testing.T) {
	dir := t.TempDir()
	htmlgraphDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	// Session completed 40 days ago — should be archived.
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	insertSession(t, db, "sess-old", "completed", &old)
	makeSessionDir(t, htmlgraphDir, "sess-old", `{"event":"test"}`+"\n")

	t.Setenv("WIPNOTE_SESSION_RETAIN_DAYS", "30")
	if err := retention.Run(db, htmlgraphDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Live session dir should be gone.
	if _, err := os.Stat(filepath.Join(htmlgraphDir, "sessions", "sess-old")); !os.IsNotExist(err) {
		t.Error("expected live session dir to be removed after archiving")
	}

	// Archive should exist somewhere under .wipnote/archive/.
	archiveRoot := filepath.Join(htmlgraphDir, "archive")
	found := false
	_ = filepath.Walk(archiveRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.Name() == "sess-old.tar.gz" {
			found = true
		}
		return nil
	})
	if !found {
		t.Error("expected archive file to exist under .wipnote/archive/")
	}
}

func TestRun_SkipsActiveSession(t *testing.T) {
	dir := t.TempDir()
	htmlgraphDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	// Active session — must not be archived regardless of age.
	old := time.Now().UTC().Add(-60 * 24 * time.Hour)
	insertSession(t, db, "sess-active", "active", &old)
	makeSessionDir(t, htmlgraphDir, "sess-active", `{"event":"live"}`+"\n")

	t.Setenv("WIPNOTE_SESSION_RETAIN_DAYS", "30")
	if err := retention.Run(db, htmlgraphDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Live dir must still exist.
	if _, err := os.Stat(filepath.Join(htmlgraphDir, "sessions", "sess-active")); err != nil {
		t.Errorf("expected active session dir to remain: %v", err)
	}
}

func TestRun_SkipsRecentCompletedSession(t *testing.T) {
	dir := t.TempDir()
	htmlgraphDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	// Completed 5 days ago — within retention window.
	recent := time.Now().UTC().Add(-5 * 24 * time.Hour)
	insertSession(t, db, "sess-recent", "completed", &recent)
	makeSessionDir(t, htmlgraphDir, "sess-recent", `{"event":"recent"}`+"\n")

	t.Setenv("WIPNOTE_SESSION_RETAIN_DAYS", "30")
	if err := retention.Run(db, htmlgraphDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Live dir must still exist.
	if _, err := os.Stat(filepath.Join(htmlgraphDir, "sessions", "sess-recent")); err != nil {
		t.Errorf("expected recent session dir to remain: %v", err)
	}
}

func TestRun_DryRunDoesNotMoveFiles(t *testing.T) {
	dir := t.TempDir()
	htmlgraphDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	insertSession(t, db, "sess-dry", "completed", &old)
	makeSessionDir(t, htmlgraphDir, "sess-dry", `{"event":"dry"}`+"\n")

	t.Setenv("WIPNOTE_SESSION_RETAIN_DAYS", "30")
	if err := retention.Run(db, htmlgraphDir, true /* dryRun */); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Dry-run: live dir must still exist.
	if _, err := os.Stat(filepath.Join(htmlgraphDir, "sessions", "sess-dry")); err != nil {
		t.Errorf("dry-run must not remove session dir: %v", err)
	}

	// Dry-run: archive must not exist.
	archiveRoot := filepath.Join(htmlgraphDir, "archive")
	if _, err := os.Stat(archiveRoot); err == nil {
		t.Error("dry-run must not create archive dir")
	}
}

func TestExtractArchive_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	htmlgraphDir := filepath.Join(dir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}

	db := openTestDB(t)

	content := `{"trace_id":"abc","span_id":"123"}` + "\n"
	old := time.Now().UTC().Add(-40 * 24 * time.Hour)
	insertSession(t, db, "sess-rt", "completed", &old)
	makeSessionDir(t, htmlgraphDir, "sess-rt", content)

	// Archive it.
	t.Setenv("WIPNOTE_SESSION_RETAIN_DAYS", "30")
	if err := retention.Run(db, htmlgraphDir, false); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Verify live dir was removed.
	if _, err := os.Stat(filepath.Join(htmlgraphDir, "sessions", "sess-rt")); !os.IsNotExist(err) {
		t.Fatal("expected session dir to be removed before restore")
	}

	// Restore.
	if err := retention.ExtractArchive(htmlgraphDir, "sess-rt"); err != nil {
		t.Fatalf("ExtractArchive: %v", err)
	}

	// Restored events.ndjson should match original content.
	got, err := os.ReadFile(filepath.Join(htmlgraphDir, "sessions", "sess-rt", "events.ndjson"))
	if err != nil {
		t.Fatalf("read restored events: %v", err)
	}
	if string(got) != content {
		t.Errorf("restored content mismatch:\n got:  %q\n want: %q", got, content)
	}
}
