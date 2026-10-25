package db

import (
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
)

// openTestDB opens (or creates) a migrated wipnote database at dbPath for this
// package's internal tests. Production has no file-backed opener
// (feat-726637b8); other packages' tests use dbtest.Open, which this package
// cannot import without a cycle.
func openTestDB(dbPath string) (*sql.DB, error) {
	dsn := dbPath
	if !strings.Contains(dbPath, ":memory:") {
		if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
			return nil, err
		}
		dsn += "?_pragma=busy_timeout(5000)&_txlock=immediate"
	}
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := ApplyPragmas(database, testPragmasFor(dbPath)); err != nil {
		database.Close()
		return nil, fmt.Errorf("applying pragmas: %w", err)
	}
	if err := RunMigrations(database); err != nil {
		database.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}
	return database, nil
}

// testPragmasFor is the standard pragma set, with DELETE journaling where WAL
// mmap is unsafe for dbPath's filesystem.
func testPragmasFor(dbPath string) map[string]string {
	p := maps.Clone(Pragmas)
	if _, walSafe := ProbeFsType(dbPath); !walSafe {
		p["journal_mode"] = "DELETE"
	}
	return p
}
