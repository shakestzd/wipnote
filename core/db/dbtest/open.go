package dbtest

import (
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
)

// Open opens (or creates) a wipnote SQLite database at dbPath with the
// standard pragmas and every migration applied. It is the test-only
// replacement for the file-backed opener production no longer has
// (feat-726637b8): production reads go through the ephemeral projection
// (dbpkg.OpenEphemeralProjection). A DSN containing ":memory:" opens an
// in-memory database.
func Open(dbPath string) (*sql.DB, error) {
	dsn := dbPath
	if !strings.Contains(dbPath, ":memory:") {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
		dsn += "?_pragma=busy_timeout(5000)&_txlock=immediate"
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	if err := dbpkg.ApplyPragmas(database, pragmasFor(dbPath)); err != nil {
		database.Close()
		return nil, fmt.Errorf("applying pragmas: %w", err)
	}
	if err := dbpkg.RunMigrations(database); err != nil {
		database.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return database, nil
}

// pragmasFor is the standard pragma set, with DELETE journaling where WAL
// mmap is unsafe for dbPath's filesystem.
func pragmasFor(dbPath string) map[string]string {
	p := maps.Clone(dbpkg.Pragmas)
	if _, walSafe := dbpkg.ProbeFsType(dbPath); !walSafe {
		p["journal_mode"] = "DELETE"
	}
	return p
}
