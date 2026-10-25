package dbtest

import (
	"database/sql"
	"time"
)

// SetActiveWorkItem records workItemID as the active item of (sessionID,
// agentID), replacing any earlier row.
func SetActiveWorkItem(database *sql.DB, sessionID, agentID, workItemID string) error {
	_, err := database.Exec(
		`INSERT OR REPLACE INTO active_work_items (session_id, agent_id, work_item_id, claimed_at)
		 VALUES (?, ?, ?, ?)`,
		sessionID, agentID, workItemID, time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	return err
}

// ActiveWorkItemsForSession returns the session's active_work_items rows,
// keyed by agent_id.
func ActiveWorkItemsForSession(database *sql.DB, sessionID string) (map[string]string, error) {
	rows, err := database.Query(
		`SELECT agent_id, work_item_id FROM active_work_items WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var agentID, workItemID string
		if err := rows.Scan(&agentID, &workItemID); err != nil {
			return nil, err
		}
		result[agentID] = workItemID
	}
	return result, rows.Err()
}
