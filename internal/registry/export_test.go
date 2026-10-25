package registry

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// Test-only helpers: these have no production caller. They live in a _test.go
// file so the package API carries only what the CLI uses.

// MigrationPending reports whether this Registry was populated via the
// legacy-path fallback and has not been saved yet.
func (r *Registry) MigrationPending() bool {
	return r != nil && r.migrated
}

// ShouldSkipRegistration reports whether dir would be skipped at registration
// time: WIPNOTE_SKIP_REGISTER=1, or a Go test tempdir path.
func ShouldSkipRegistration(projectDir string) bool {
	if os.Getenv("WIPNOTE_SKIP_REGISTER") == "1" {
		return true
	}
	return isGoTestTempDirPath(projectDir)
}

// OpenReadOnly opens the SQLite database at dbPath with ?mode=ro. No
// migrations or PRAGMAs are applied. The caller closes the returned *sql.DB.
func OpenReadOnly(dbPath string) (*sql.DB, error) {
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("registry.OpenReadOnly: resolve path: %w", err)
	}
	dsn := fmt.Sprintf("file:%s?mode=ro&_busy_timeout=5000", abs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("registry.OpenReadOnly: open: %w", err)
	}
	return db, nil
}
