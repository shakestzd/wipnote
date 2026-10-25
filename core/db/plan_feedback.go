package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"time"
)

// PlanFeedback represents a single feedback entry captured from a CRISPI plan review.
//
// Block-level annotations (slice-8) reuse this same row shape with
// Action=="annotation". For those rows:
//
//   - Anchor pins the note to a specific plan block (e.g. "slice-3-block-data-model-1").
//   - Consumed and Resolved are two INDEPENDENT axes: an agent may consume a
//     note (ingest it) without it being resolved (addressed), and vice-versa.
//   - ResolutionTarget routes the note to "agent" or "human".
//
// Non-annotation rows (approve / comment / answer) leave these at their zero
// values, exactly as they were before the columns existed.
type PlanFeedback struct {
	ID               int64
	PlanID           string
	Section          string
	Action           string
	Value            string
	QuestionID       string
	Anchor           string
	Consumed         bool
	Resolved         bool
	ResolutionTarget string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// PlanAnnotation is the block-anchored, two-axis annotation written by
// StorePlanAnnotation. It is the typed input shape for the slice-8 annotation
// loop; on disk it is just a plan_feedback row with Action=="annotation".
type PlanAnnotation struct {
	Section          string // block-anchor section key (e.g. slice-3-block-data-model-1)
	Anchor           string // block/element anchor the note pins to
	Value            string // the reviewer's comment text
	QuestionID       string // optional stable id for upsert addressing
	Consumed         bool   // axis 1: an agent has ingested the note
	Resolved         bool   // axis 2: the note has been addressed
	ResolutionTarget string // routed: "agent" | "human" | ""
}

// IsPlanApprovalSection returns true if the section is one of the approvable sections
// exposed in the CRISPI plan UI: "design", "outline", or slice-N (where N is numeric).
// All other sections (slice-N-question-*, critique, q-*, meta, chat, etc.) are excluded.
func IsPlanApprovalSection(section string) bool {
	if section == "design" || section == "outline" {
		return true
	}
	// Match slice-N where N is one or more digits.
	matched, err := regexp.MatchString(`^slice-\d+$`, section)
	return err == nil && matched
}

// IsPlanApprovalValueApproved reports whether a stored plan_feedback approve
// value represents approval. "approved" is a legacy UI value that may still be
// visible to already-open DB handles that predate normalization.
func IsPlanApprovalValueApproved(value string) bool {
	switch value {
	case "true", "approved":
		return true
	default:
		return false
	}
}

// PlanSliceApproval is a minimal slice descriptor used by canonical-first approval
// checks. Callers in cmd/ populate this from planyaml.PlanSlice to avoid a
// circular import (cmd/ → plan/planyaml is fine; core/db → plan/planyaml is not).
type PlanSliceApproval struct {
	Num            int
	ApprovalStatus string // "approved" | "rejected" | "pending" | ""
	Approved       bool   // legacy bool field, written by finalize-yaml
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
		// Only include actual slice-N sections (not slice-N-question-*).
		if IsPlanApprovalSection(section) {
			if IsPlanApprovalValueApproved(value) {
				result[section] = "approved"
			} else {
				result[section] = "rejected"
			}
		}
	}
	return result, rows.Err()
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
