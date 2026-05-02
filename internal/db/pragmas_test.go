package db_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	db "github.com/shakestzd/htmlgraph/internal/db"
	_ "modernc.org/sqlite"
)

// TestApplyPragmas_BusyTimeoutBeforeJournalMode verifies that Open/ApplyPragmas
// succeeds even when another goroutine holds an exclusive BEGIN IMMEDIATE lock.
// Before the fix, PRAGMA journal_mode would fail with SQLITE_BUSY because
// busy_timeout had not yet been applied.
func TestApplyPragmas_BusyTimeoutBeforeJournalMode(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Seed the database file so a second open can acquire a lock immediately.
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE IF NOT EXISTS _seed (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("seed create: %v", err)
	}
	seed.Close()

	// Goroutine: hold an exclusive write lock for ~150ms.
	var wg sync.WaitGroup
	wg.Add(1)
	ready := make(chan struct{})
	go func() {
		defer wg.Done()
		locker, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Errorf("locker open: %v", err)
			close(ready)
			return
		}
		defer locker.Close()
		locker.SetMaxOpenConns(1)

		if _, err := locker.Exec(`BEGIN IMMEDIATE`); err != nil {
			t.Errorf("locker BEGIN IMMEDIATE: %v", err)
			close(ready)
			return
		}
		close(ready) // signal main goroutine to proceed
		time.Sleep(150 * time.Millisecond)
		locker.Exec(`ROLLBACK`) //nolint:errcheck
	}()

	// Wait until the locker has acquired its lock.
	<-ready

	// Now call Open from the main goroutine. With busy_timeout applied first,
	// SQLite will wait up to 5 s and succeed once the lock is released.
	database, openErr := db.Open(dbPath)

	wg.Wait()

	if openErr != nil {
		t.Fatalf("Open failed under contention (busy_timeout not applied before journal_mode): %v", openErr)
	}
	database.Close()
	os.Remove(dbPath)
}
