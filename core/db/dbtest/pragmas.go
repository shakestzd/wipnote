package dbtest

import "database/sql"

// JournalMode returns the effective journal_mode of the connection, or
// "unknown" if the query fails.
func JournalMode(database *sql.DB) string {
	var mode string
	if err := database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return "unknown"
	}
	return mode
}
