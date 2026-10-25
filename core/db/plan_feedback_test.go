package db_test

import (
	"database/sql"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/shakestzd/wipnote/core/db"
)

// setupPlanDB returns an in-memory database with a test plan feature row.
func setupPlanDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	database := setupTestDB(t)
	planID := "plan-test-001"
	_, err := database.Exec(
		`INSERT INTO features (id, type, title, status) VALUES (?, 'plan', 'Test Plan', 'in-progress')`,
		planID,
	)
	if err != nil {
		t.Fatalf("insert test plan: %v", err)
	}
	return database, planID
}

// ---- IsPlanFullyApproved regression tests (slice-4) ----------------------------

// ---- NormalizePlanFeedbackValues tests (bug-4b399212) ---------------------------

// TestNormalizePlanFeedbackValues verifies that existing rows written by the buggy
// slice-card UI (value='approved'/'rejected'/'changes_requested') are mapped to the
// canonical boolean strings ('true'/'false'), and that the migration is idempotent.
func TestNormalizePlanFeedbackValues(t *testing.T) {
	database, planID := setupPlanDB(t)
	defer database.Close()

	// Insert raw rows bypassing StorePlanFeedback to simulate the buggy UI writer.
	_, err := database.Exec(`
		INSERT INTO plan_feedback (plan_id, section, action, value, question_id, created_at, updated_at)
		VALUES (?, 'slice-1', 'approve', 'approved', '', datetime('now'), datetime('now'))`,
		planID)
	if err != nil {
		t.Fatalf("insert approved row: %v", err)
	}
	_, err = database.Exec(`
		INSERT INTO plan_feedback (plan_id, section, action, value, question_id, created_at, updated_at)
		VALUES (?, 'slice-2', 'approve', 'rejected', '', datetime('now'), datetime('now'))`,
		planID)
	if err != nil {
		t.Fatalf("insert rejected row: %v", err)
	}
	_, err = database.Exec(`
		INSERT INTO plan_feedback (plan_id, section, action, value, question_id, created_at, updated_at)
		VALUES (?, 'slice-3', 'approve', 'changes_requested', '', datetime('now'), datetime('now'))`,
		planID)
	if err != nil {
		t.Fatalf("insert changes_requested row: %v", err)
	}

	// Run migration.
	if err := db.NormalizePlanFeedbackValues(database); err != nil {
		t.Fatalf("NormalizePlanFeedbackValues: %v", err)
	}

	// Verify values are now canonical.
	checkValue := func(section, want string) {
		t.Helper()
		var got string
		err := database.QueryRow(
			`SELECT value FROM plan_feedback WHERE plan_id=? AND section=? AND action='approve'`,
			planID, section,
		).Scan(&got)
		if err != nil {
			t.Fatalf("query %s: %v", section, err)
		}
		if got != want {
			t.Errorf("section %s: got %q, want %q", section, got, want)
		}
	}
	checkValue("slice-1", "true")
	checkValue("slice-2", "false")
	checkValue("slice-3", "false")

	// Run migration again — idempotency check, should not error and values unchanged.
	if err := db.NormalizePlanFeedbackValues(database); err != nil {
		t.Fatalf("NormalizePlanFeedbackValues (second run): %v", err)
	}
	checkValue("slice-1", "true")
	checkValue("slice-2", "false")
	checkValue("slice-3", "false")
}

// ---- GetSliceApprovals tests (slice-4) -----------------------------------------

// TestGetSliceApprovals_ReturnsPerSliceStatus verifies that GetSliceApprovals
// returns only slice-N keyed rows and maps them to approval status correctly.
func TestGetSliceApprovals_ReturnsPerSliceStatus(t *testing.T) {
	database, planID := setupPlanDB(t)
	defer database.Close()

	// Store mixed sections: design (legacy) and two slices.
	if err := dbtest.StorePlanFeedback(database, planID, "design", "approve", "true", ""); err != nil {
		t.Fatalf("store design: %v", err)
	}
	if err := dbtest.StorePlanFeedback(database, planID, "slice-1", "approve", "true", ""); err != nil {
		t.Fatalf("store slice-1: %v", err)
	}
	if err := dbtest.StorePlanFeedback(database, planID, "slice-2", "approve", "false", ""); err != nil {
		t.Fatalf("store slice-2: %v", err)
	}

	approvals, err := db.GetSliceApprovals(database, planID)
	if err != nil {
		t.Fatalf("GetSliceApprovals: %v", err)
	}

	// Should only contain slice sections, not "design".
	if _, hasDesign := approvals["design"]; hasDesign {
		t.Error("GetSliceApprovals should not include legacy 'design' section")
	}
	if v, ok := approvals["slice-1"]; !ok || v != "approved" {
		t.Errorf("slice-1: got %q ok=%v, want approved=true", v, ok)
	}
	if v, ok := approvals["slice-2"]; !ok || v != "rejected" {
		t.Errorf("slice-2: got %q ok=%v, want rejected", v, ok)
	}
}

// ---- IsPlanFullyApproved regression tests (bug-9e45dd31) ---------------------------

// ---- Canonical-first approval tests (bug-eca8141d) ---------------------------
