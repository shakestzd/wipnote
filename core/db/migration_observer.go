package db

import (
	_ "modernc.org/sqlite"
)

// migrationObserver is an optional hook called each time a migration step
// fires. Only tests install it; production leaves it nil.
var migrationObserver func(name string)

// SetMigrationObserver installs a migration observer for testing. Pass nil to
// remove a previously installed observer. It is not concurrency-safe: install
// observers only at test setup. Nothing in production calls it (deadcode
// reports it); feat-726637b8 keeps it as the seam the migration tests use.
func SetMigrationObserver(fn func(name string)) {
	migrationObserver = fn
}

// notifyMigration calls the migration observer if one is installed.
func notifyMigration(name string) {
	if migrationObserver != nil {
		migrationObserver(name)
	}
}
