package dbtest

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"time"
)

// UpsertSessionFile records a claimless file touch in session_files,
// updating operation and last_seen on a repeat touch of the same path.
func UpsertSessionFile(database *sql.DB, sessionID, filePath, operation string) error {
	if operation == "" {
		operation = "unknown"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	h := sha256.Sum256([]byte(filePath))
	id := fmt.Sprintf("%s-%x", sessionID, h[:4])
	_, err := database.Exec(`
		INSERT INTO session_files (id, session_id, file_path, operation, first_seen, last_seen, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id, file_path) DO UPDATE SET
			operation = excluded.operation,
			last_seen = excluded.last_seen`,
		id, sessionID, filePath, operation, now, now, now)
	return err
}
