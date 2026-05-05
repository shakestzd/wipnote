package db_test

import (
	"context"
	"path/filepath"
	"testing"

	db "github.com/shakestzd/htmlgraph/internal/db"
	_ "modernc.org/sqlite"
)

// TestApplyPragmas_AppliesBusyTimeout verifies that Open applies busy_timeout
// to the database connection. Rather than relying on lock-contention timing
// (which is non-deterministic across CI environments), we query the PRAGMA
// value directly after opening and assert it equals the configured value.
func TestApplyPragmas_AppliesBusyTimeout(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()

	ctx := context.Background()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("Conn: %v", err)
	}
	defer conn.Close()

	var busyTimeout int
	row := conn.QueryRowContext(ctx, "PRAGMA busy_timeout")
	if err := row.Scan(&busyTimeout); err != nil {
		t.Fatalf("PRAGMA busy_timeout scan: %v", err)
	}

	const wantBusyTimeout = 5000
	if busyTimeout != wantBusyTimeout {
		t.Errorf("busy_timeout = %d, want %d", busyTimeout, wantBusyTimeout)
	}
}
