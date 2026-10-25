package db_test

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// setupClaimDB returns an in-memory database with a test session and feature.
func setupClaimDB(t *testing.T) *sql.DB {
	t.Helper()
	database := setupTestDB(t)
	_, err := database.Exec(
		`INSERT INTO features (id, type, title, status) VALUES ('feat-test', 'feature', 'Test Feature', 'in-progress')`,
	)
	if err != nil {
		t.Fatalf("insert test feature: %v", err)
	}
	return database
}

// makeClaim returns a minimal Claim for testing.
func makeClaim(id, workItemID, sessionID string) *models.Claim {
	return &models.Claim{
		ClaimID:        id,
		WorkItemID:     workItemID,
		OwnerSessionID: sessionID,
		OwnerAgent:     "claude-code",
		Status:         models.ClaimProposed,
	}
}

func TestHeartbeatClaim(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-hb1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 5*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	before, err := db.GetClaim(database, "claim-hb1")
	if err != nil {
		t.Fatalf("GetClaim before: %v", err)
	}

	// Extend by 30 minutes.
	if err := db.HeartbeatClaim(database, "claim-hb1", "sess-test", 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaim: %v", err)
	}

	after, err := db.GetClaim(database, "claim-hb1")
	if err != nil {
		t.Fatalf("GetClaim after: %v", err)
	}
	if !after.LeaseExpiresAt.After(before.LeaseExpiresAt) {
		t.Errorf("lease_expires_at not extended: before=%v after=%v",
			before.LeaseExpiresAt, after.LeaseExpiresAt)
	}
}

func TestHeartbeatClaimWrongSession(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-hbws1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	err := db.HeartbeatClaim(database, "claim-hbws1", "sess-wrong", 30*time.Minute)
	if err == nil {
		t.Fatal("expected error when heartbeating with wrong session, got nil")
	}
}

func TestReleaseClaim(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-rel1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	if err := db.ReleaseClaim(database, "claim-rel1", "sess-test", models.ClaimCompleted); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}

	got, err := db.GetClaim(database, "claim-rel1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Status != models.ClaimCompleted {
		t.Errorf("status: got %q, want %q", got.Status, models.ClaimCompleted)
	}

	// Active claim should now be gone.
	active, err := activeClaim(database, "feat-test")
	if err != nil {
		t.Fatalf("GetActiveClaim after release: %v", err)
	}
	if active != nil {
		t.Errorf("expected no active claim after release, got %v", active)
	}
}

func TestListActiveClaimsBySession(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	// Insert a second feature for a second claim.
	_, err := database.Exec(
		`INSERT INTO features (id, type, title, status) VALUES ('feat-list-2', 'feature', 'List Feature 2', 'in-progress')`,
	)
	if err != nil {
		t.Fatalf("insert feat-list-2: %v", err)
	}

	c1 := makeClaim("claim-list1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c1, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem c1: %v", err)
	}
	c2 := makeClaim("claim-list2", "feat-list-2", "sess-test")
	if err := dbtest.InsertClaim(database, c2, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem c2: %v", err)
	}

	claims, err := db.ListActiveClaimsBySession(database, "sess-test")
	if err != nil {
		t.Fatalf("ListActiveClaimsBySession: %v", err)
	}
	if len(claims) != 2 {
		t.Errorf("count: got %d, want 2", len(claims))
	}

	// Release one and verify the count drops.
	if err := db.ReleaseClaim(database, "claim-list1", "sess-test", models.ClaimCompleted); err != nil {
		t.Fatalf("ReleaseClaim: %v", err)
	}
	claims, err = db.ListActiveClaimsBySession(database, "sess-test")
	if err != nil {
		t.Fatalf("ListActiveClaimsBySession after release: %v", err)
	}
	if len(claims) != 1 {
		t.Errorf("count after release: got %d, want 1", len(claims))
	}
}

// unmarshalWriteScope is a test helper that unmarshals the write_scope JSON of a claim.
func unmarshalWriteScope(t *testing.T, raw json.RawMessage) models.WriteScope {
	t.Helper()
	var scope models.WriteScope
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &scope); err != nil {
			t.Fatalf("unmarshal write_scope: %v (raw: %s)", err, string(raw))
		}
	}
	return scope
}

func TestListClaims(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	// Insert a second session and feature.
	_, err := database.Exec(
		`INSERT INTO sessions (session_id, agent_assigned, created_at, status) VALUES ('sess-b', 'claude-code', ?, 'active')`,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert sess-b: %v", err)
	}
	_, err = database.Exec(
		`INSERT INTO features (id, type, title, status) VALUES ('feat-lc-2', 'feature', 'LC Feature 2', 'in-progress')`,
	)
	if err != nil {
		t.Fatalf("insert feat-lc-2: %v", err)
	}

	ca := makeClaim("claim-lc-a", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, ca, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem ca: %v", err)
	}
	cb := makeClaim("claim-lc-b", "feat-lc-2", "sess-b")
	if err := dbtest.InsertClaim(database, cb, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem cb: %v", err)
	}
	// Release ca so it becomes completed.
	if err := db.ReleaseClaim(database, "claim-lc-a", "sess-test", models.ClaimCompleted); err != nil {
		t.Fatalf("ReleaseClaim ca: %v", err)
	}

	t.Run("all", func(t *testing.T) {
		all, err := db.ListClaims(database, "", "", 100)
		if err != nil {
			t.Fatalf("ListClaims all: %v", err)
		}
		if len(all) < 2 {
			t.Errorf("expected >=2 claims, got %d", len(all))
		}
	})

	t.Run("by_session", func(t *testing.T) {
		bySession, err := db.ListClaims(database, "sess-test", "", 100)
		if err != nil {
			t.Fatalf("ListClaims by session: %v", err)
		}
		if len(bySession) != 1 {
			t.Errorf("by session: got %d, want 1", len(bySession))
		}
		if bySession[0].ClaimID != "claim-lc-a" {
			t.Errorf("claim_id: got %q, want %q", bySession[0].ClaimID, "claim-lc-a")
		}
	})

	t.Run("by_status", func(t *testing.T) {
		completed, err := db.ListClaims(database, "", "completed", 100)
		if err != nil {
			t.Fatalf("ListClaims by status: %v", err)
		}
		if len(completed) != 1 {
			t.Errorf("completed status: got %d, want 1", len(completed))
		}
	})

	t.Run("limit", func(t *testing.T) {
		limited, err := db.ListClaims(database, "", "", 1)
		if err != nil {
			t.Fatalf("ListClaims limit: %v", err)
		}
		if len(limited) != 1 {
			t.Errorf("limit 1: got %d, want 1", len(limited))
		}
	})
}

// activeClaim returns the newest active claim for workItemID, or nil when none.
func activeClaim(database *sql.DB, workItemID string) (*models.Claim, error) {
	quoted := make([]string, len(models.ActiveClaimStatuses))
	for i, st := range models.ActiveClaimStatuses {
		quoted[i] = "'" + string(st) + "'"
	}
	var id string
	err := database.QueryRow(`SELECT claim_id FROM claims WHERE work_item_id = ? AND status IN (`+
		strings.Join(quoted, ",")+`) ORDER BY created_at DESC LIMIT 1`, workItemID).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return db.GetClaim(database, id)
}
