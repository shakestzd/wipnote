// Package db provides SQLite database operations for HtmlGraph.
//
// Uses modernc.org/sqlite (pure Go, no CGo) for maximum portability.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"maps"
)

// Pragmas mirrors the Python PRAGMA_SETTINGS from pragmas.py.
// journal_mode is intentionally omitted here; BuildPragmas sets it based on
// filesystem type detection (WAL on safe native fs, DELETE on unsafe/unknown).
var Pragmas = map[string]string{
	"journal_mode": "WAL",
	"synchronous":  "NORMAL",
	"foreign_keys": "1",
	"busy_timeout": "5000",
	"cache_size":   "-64000",
	"temp_store":   "MEMORY",
	"mmap_size":    "0",
}

// BuildPragmas returns a pragma map with journal_mode resolved for the given
// database path. On filesystems where WAL-mode mmap is unsafe (virtiofs, FUSE,
// 9p, overlayfs, NFS, SMB), journal_mode is set to DELETE to avoid SIGBUS
// crashes. On safelisted native filesystems it stays WAL.
func BuildPragmas(dbPath string) map[string]string {
	p := make(map[string]string, len(Pragmas))
	maps.Copy(p, Pragmas)
	if isUnsafeForMmap(dbPath) {
		p["journal_mode"] = "DELETE"
	}
	return p
}

// ApplyPragmas sets all performance PRAGMAs on a database connection.
// Some PRAGMAs (busy_timeout, cache_size) are best-effort and may not apply
// to all backing stores (e.g., in-memory SQLite); failures are logged at debug
// level and don't block the Open. Other PRAGMAs are required.
func ApplyPragmas(db *sql.DB, pragmas map[string]string) error {
	// Must run before journal_mode: if the write lock is held, busy_timeout
	// tells SQLite how long to wait before returning SQLITE_BUSY.
	if bt, ok := pragmas["busy_timeout"]; ok {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout = %s", bt)); err != nil {
			log.Printf("debug: skipping PRAGMA busy_timeout (not supported on this backing): %v", err)
		}
	}

	// PRAGMAs that are REQUIRED — fail Open if these don't apply.
	required := []string{"journal_mode", "synchronous", "foreign_keys", "temp_store", "mmap_size"}
	// PRAGMAs that are best-effort — failure is logged at debug level
	// and doesn't block Open (some drivers/backing stores reject these).
	// busy_timeout is handled above (must precede journal_mode), so omit it here.
	optional := []string{"cache_size"}

	for _, pragma := range required {
		value, ok := pragmas[pragma]
		if !ok {
			continue
		}
		_, err := db.Exec(fmt.Sprintf("PRAGMA %s = %s", pragma, value))
		if err != nil {
			return fmt.Errorf("applying PRAGMA %s: %w", pragma, err)
		}
	}

	for _, pragma := range optional {
		value, ok := pragmas[pragma]
		if !ok {
			continue
		}
		_, err := db.Exec(fmt.Sprintf("PRAGMA %s = %s", pragma, value))
		if err != nil {
			// Best-effort: log at debug, continue. In-memory DBs in tests
			// may reject busy_timeout / cache_size; that's fine because
			// they aren't subject to the contention these PRAGMAs protect
			// against.
			log.Printf("debug: skipping PRAGMA %s (not supported on this backing): %v", pragma, err)
		}
	}
	return nil
}

// RunOptimize executes PRAGMA optimize for planner/statistics upkeep.
func RunOptimize(db *sql.DB) error {
	_, err := db.Exec("PRAGMA optimize")
	return err
}

// CheckIntegrity runs integrity_check and foreign_key_check.
// Returns true if the database passes both checks.
func CheckIntegrity(db *sql.DB) (bool, error) {
	row := db.QueryRow("PRAGMA integrity_check")
	var result string
	if err := row.Scan(&result); err != nil {
		return false, fmt.Errorf("integrity_check: %w", err)
	}
	if result != "ok" {
		return false, nil
	}

	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return false, fmt.Errorf("foreign_key_check: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		// Any row means a violation exists.
		return false, nil
	}
	return true, rows.Err()
}
