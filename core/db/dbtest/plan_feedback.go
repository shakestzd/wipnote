package dbtest

import (
	"database/sql"
	"time"
)

// StorePlanFeedback upserts one plan_feedback row keyed on
// (plan_id, section, action, question_id).
func StorePlanFeedback(database *sql.DB, planID, section, action, value, questionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := database.Exec(`
		INSERT INTO plan_feedback (plan_id, section, action, value, question_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(plan_id, section, action, question_id) DO UPDATE SET
			value      = excluded.value,
			updated_at = excluded.updated_at`,
		planID, section, action, nullable(value), questionID, now, now)
	return err
}

// PlanFeedbackByAction returns the plan's plan_feedback values keyed by action
// (the last row wins when an action repeats).
func PlanFeedbackByAction(database *sql.DB, planID string) (map[string]string, error) {
	rows, err := database.Query(
		`SELECT action, COALESCE(value, '') FROM plan_feedback WHERE plan_id = ? ORDER BY created_at ASC`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var action, value string
		if err := rows.Scan(&action, &value); err != nil {
			return nil, err
		}
		out[action] = value
	}
	return out, rows.Err()
}
