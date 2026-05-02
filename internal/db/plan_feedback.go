package db

import (
	"database/sql"
	"fmt"
	"time"
)

// PlanFeedback represents a single feedback entry captured from a CRISPI plan review.
type PlanFeedback struct {
	ID         int64
	PlanID     string
	Section    string
	Action     string
	Value      string
	QuestionID string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// StorePlanFeedback upserts a feedback entry for a plan section.
// Re-submitting feedback for the same (plan_id, section, action, question_id)
// updates the existing row rather than creating a duplicate.
func StorePlanFeedback(db *sql.DB, planID, section, action, value, questionID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		INSERT INTO plan_feedback (plan_id, section, action, value, question_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(plan_id, section, action, question_id) DO UPDATE SET
			value      = excluded.value,
			updated_at = excluded.updated_at`,
		planID, section, action, nullStr(value), questionID, now, now,
	)
	if err != nil {
		return fmt.Errorf("store plan feedback (plan=%s section=%s action=%s): %w", planID, section, action, err)
	}
	return nil
}

// GetPlanFeedback retrieves all feedback entries for a plan, ordered by created_at.
func GetPlanFeedback(db *sql.DB, planID string) ([]PlanFeedback, error) {
	rows, err := db.Query(`
		SELECT id, plan_id, section, action, value, question_id, created_at, updated_at
		FROM plan_feedback
		WHERE plan_id = ?
		ORDER BY created_at ASC`, planID)
	if err != nil {
		return nil, fmt.Errorf("get plan feedback (plan=%s): %w", planID, err)
	}
	defer rows.Close()
	return scanPlanFeedbackRows(rows)
}

// GetPlanFeedbackBySection retrieves feedback for a specific section of a plan.
func GetPlanFeedbackBySection(db *sql.DB, planID, section string) ([]PlanFeedback, error) {
	rows, err := db.Query(`
		SELECT id, plan_id, section, action, value, question_id, created_at, updated_at
		FROM plan_feedback
		WHERE plan_id = ? AND section = ?
		ORDER BY created_at ASC`, planID, section)
	if err != nil {
		return nil, fmt.Errorf("get plan feedback by section (plan=%s section=%s): %w", planID, section, err)
	}
	defer rows.Close()
	return scanPlanFeedbackRows(rows)
}

// IsPlanFullyApproved returns true when every distinct section in plan_feedback
// has at least one 'approve' entry with value 'true'.
// Returns false (not an error) when no sections exist yet.
func IsPlanFullyApproved(db *sql.DB, planID string) (bool, error) {
	// Count distinct sections that have been explicitly approved.
	var approvedSections int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT section)
		FROM plan_feedback
		WHERE plan_id = ? AND action = 'approve' AND value = 'true'`,
		planID,
	).Scan(&approvedSections)
	if err != nil {
		return false, fmt.Errorf("count approved sections (plan=%s): %w", planID, err)
	}
	if approvedSections == 0 {
		return false, nil
	}

	// All known sections must be approved — none may be disapproved (value != 'true').
	var unapprovedSections int
	err = db.QueryRow(`
		SELECT COUNT(DISTINCT section)
		FROM plan_feedback
		WHERE plan_id = ? AND action = 'approve' AND value != 'true'`,
		planID,
	).Scan(&unapprovedSections)
	if err != nil {
		return false, fmt.Errorf("count unapproved sections (plan=%s): %w", planID, err)
	}

	return unapprovedSections == 0, nil
}

// FinalizePlan marks a plan as done. If the plan exists in the features table,
// updates its status there. Plans that exist only as HTML files (not indexed)
// are finalized successfully without a features table update.
func FinalizePlan(db *sql.DB, planID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE features SET status = 'done', updated_at = ?
		WHERE id = ? AND type = 'plan'`,
		now, planID,
	)
	if err != nil {
		return fmt.Errorf("finalize plan %s: %w", planID, err)
	}
	// Best-effort: don't fail if plan isn't in features table.
	// HTML is canonical — the on-disk HTML file gets data-status="finalized".
	return nil
}

// GetSliceApprovals returns a map of section key → approval status string
// ("approved" or "rejected") for all slice-N keyed feedback rows for the plan.
// Only sections matching the slice-<num> pattern are returned; legacy design/
// outline/questions sections are excluded.
// This is a read-only helper introduced in slice-4 for the approve-slice /
// reject-slice lifecycle commands.
func GetSliceApprovals(db *sql.DB, planID string) (map[string]string, error) {
	rows, err := db.Query(`
		SELECT section, value
		FROM plan_feedback
		WHERE plan_id = ? AND action = 'approve' AND section LIKE 'slice-%'
		ORDER BY section ASC`, planID)
	if err != nil {
		return nil, fmt.Errorf("get slice approvals (plan=%s): %w", planID, err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var section, value string
		if err := rows.Scan(&section, &value); err != nil {
			return nil, fmt.Errorf("scan slice approval row: %w", err)
		}
		if value == "true" {
			result[section] = "approved"
		} else {
			result[section] = "rejected"
		}
	}
	return result, rows.Err()
}

// DeletePlanFeedback deletes all feedback entries for a plan.
func DeletePlanFeedback(db *sql.DB, planID string) error {
	_, err := db.Exec(`DELETE FROM plan_feedback WHERE plan_id = ?`, planID)
	if err != nil {
		return fmt.Errorf("delete plan feedback (plan=%s): %w", planID, err)
	}
	return nil
}

// NormalizePlanFeedbackValues migrates existing rows that were written by the
// slice-card UI before the value-mapping fix. The buggy writer stored display
// values ('approved', 'changes_requested', 'rejected') instead of the canonical
// boolean strings ('true', 'false'). This function normalizes them. It is safe
// to call repeatedly — once migrated, no rows match the WHERE clauses.
func NormalizePlanFeedbackValues(db *sql.DB) error {
	if _, err := db.Exec(`UPDATE plan_feedback SET value='true' WHERE action='approve' AND value='approved'`); err != nil {
		return fmt.Errorf("normalize plan_feedback approved→true: %w", err)
	}
	if _, err := db.Exec(`UPDATE plan_feedback SET value='false' WHERE action='approve' AND value IN ('rejected','changes_requested')`); err != nil {
		return fmt.Errorf("normalize plan_feedback rejected/changes_requested→false: %w", err)
	}
	return nil
}

// scanPlanFeedbackRows scans a *sql.Rows cursor into a []PlanFeedback slice.
func scanPlanFeedbackRows(rows *sql.Rows) ([]PlanFeedback, error) {
	var results []PlanFeedback
	for rows.Next() {
		var pf PlanFeedback
		var value sql.NullString
		var createdStr, updatedStr string
		if err := rows.Scan(
			&pf.ID, &pf.PlanID, &pf.Section, &pf.Action,
			&value, &pf.QuestionID, &createdStr, &updatedStr,
		); err != nil {
			return nil, fmt.Errorf("scan plan feedback row: %w", err)
		}
		pf.Value = value.String
		pf.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		pf.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
		results = append(results, pf)
	}
	return results, rows.Err()
}
