package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"
)

// openReindexTestDB creates an in-memory SQLite database with the full schema.
func openReindexTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbtest.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

// writeMinimalFeatureHTML writes a minimal valid feature HTML file to dir/filename.
func writeMinimalFeatureHTML(t *testing.T, dir, filename, id, title string) string {
	t.Helper()
	content := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>%s</title></head>
<body>
  <article id="%s"
           data-type="feature"
           data-status="todo"
           data-priority="medium"
           data-created="%s"
           data-updated="%s">
    <header><h1>%s</h1></header>
  </article>
</body>
</html>`, title, id, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339), title)

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write feature HTML %s: %v", path, err)
	}
	return path
}

// writeMinimalTrackHTML writes a minimal valid track HTML file to dir/filename.
func writeMinimalTrackHTML(t *testing.T, dir, filename, id, title string) string {
	t.Helper()
	content := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><title>%s</title></head>
<body>
  <article id="%s"
           data-type="track"
           data-status="todo"
           data-priority="medium"
           data-created="%s"
           data-updated="%s">
    <header><h1>%s</h1></header>
  </article>
</body>
</html>`, title, id, time.Now().Format(time.RFC3339), time.Now().Format(time.RFC3339), title)

	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write track HTML %s: %v", path, err)
	}
	return path
}

// setupWipnoteDir creates a minimal .wipnote directory structure in a temp dir.
func setupWipnoteDir(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	return hgDir
}

// TestReindex_DeletedHTMLAbsentAfterRebuild pins what replaced the old stale
// purge: reindex builds a fresh projection from the files that exist, so a work
// item whose HTML was deleted has no row after the next rebuild.
func TestReindex_DeletedHTMLAbsentAfterRebuild(t *testing.T) {
	hgDir := setupWipnoteDir(t)
	writeMinimalTrackHTML(t, filepath.Join(hgDir, "tracks"), "trk-del-001.html", "trk-del-001", "Track To Delete")
	writeMinimalFeatureHTML(t, filepath.Join(hgDir, "features"), "feat-del-001.html", "feat-del-001", "Feature To Delete")
	writeMinimalFeatureHTML(t, filepath.Join(hgDir, "features"), "feat-keep-001.html", "feat-keep-001", "Feature To Keep")

	count := func(db *sql.DB, table, id string) int {
		var n int
		db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE id = ?`, id).Scan(&n)
		return n
	}
	first := openReindexTestDB(t)
	if _, err := rebuildFullProjection(first, hgDir, false, io.Discard, io.Discard); err != nil {
		t.Fatalf("first rebuild: %v", err)
	}
	if count(first, "tracks", "trk-del-001") != 1 || count(first, "features", "feat-del-001") != 1 {
		t.Fatal("initial rebuild did not index the track and feature")
	}

	os.Remove(filepath.Join(hgDir, "tracks", "trk-del-001.html"))
	os.Remove(filepath.Join(hgDir, "features", "feat-del-001.html"))

	second := openReindexTestDB(t)
	if _, err := rebuildFullProjection(second, hgDir, false, io.Discard, io.Discard); err != nil {
		t.Fatalf("second rebuild: %v", err)
	}
	if count(second, "tracks", "trk-del-001") != 0 || count(second, "features", "feat-del-001") != 0 {
		t.Error("deleted work items still present after rebuild")
	}
	if count(second, "features", "feat-keep-001") != 1 {
		t.Error("surviving feature missing after rebuild")
	}
}
