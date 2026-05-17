package db_test

// Tests for parallel attribution, collision detection, and out-of-order
// subagent attribution (slice-5, feat-6d8110b1, plan-1670cacd).

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/models"
)

// TestClaimCollisionPolicy — two root sessions claiming same work item:
// - First ClaimItemOrRenew succeeds.
// - Second ClaimItemOrRenew also succeeds (warn-and-allow, no block).
// - ListClaimsForWorkItem returns both active claims.
// - DetectCollaboration reports HasCollision=true with both sessions listed.
func TestClaimCollisionPolicy(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	insertExtraFeature(t, database, "feat-collab")
	insertExtraSession(t, database, "sess-root-A", "claude-code")
	insertExtraSession(t, database, "sess-root-B", "codex-cli")

	c1 := &models.Claim{
		ClaimID:          "clm-col-A",
		WorkItemID:       "feat-collab",
		OwnerSessionID:   "sess-root-A",
		OwnerAgent:       "claude-code",
		ClaimedByAgentID: "agent-A",
		Status:           models.ClaimInProgress,
	}
	if err := db.ClaimItemOrRenew(database, c1, 30*time.Minute); err != nil {
		t.Fatalf("first claim: %v", err)
	}

	// Second root session — warn-and-allow: must NOT return an error.
	c2 := &models.Claim{
		ClaimID:          "clm-col-B",
		WorkItemID:       "feat-collab",
		OwnerSessionID:   "sess-root-B",
		OwnerAgent:       "codex-cli",
		ClaimedByAgentID: "agent-B",
		Status:           models.ClaimInProgress,
	}
	if err := db.ClaimItemOrRenew(database, c2, 30*time.Minute); err != nil {
		t.Fatalf("second claim (warn-and-allow must succeed): %v", err)
	}

	claimsForItem, err := db.ListClaimsForWorkItem(database, "feat-collab")
	if err != nil {
		t.Fatalf("ListClaimsForWorkItem: %v", err)
	}
	if len(claimsForItem) < 2 {
		t.Errorf("expected >=2 active claims, got %d", len(claimsForItem))
	}

	coll, err := db.DetectCollaboration(database, "feat-collab")
	if err != nil {
		t.Fatalf("DetectCollaboration: %v", err)
	}
	if !coll.HasCollision {
		t.Error("expected HasCollision=true")
	}
	if len(coll.Claimants) < 2 {
		t.Errorf("expected >=2 claimants, got %d", len(coll.Claimants))
	}
}

// TestParallelHarnessClaims — three harnesses (Claude, Codex, Gemini) claim the
// same work item concurrently. All three must coexist; collaboration state shows
// all three with correct attribution (session, agent/harness).
func TestParallelHarnessClaims(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	insertExtraFeature(t, database, "feat-parallel")
	for _, s := range []struct{ id, harness string }{
		{"sess-claude-ph", "claude-code"},
		{"sess-codex-ph", "codex-cli"},
		{"sess-gemini-ph", "gemini-cli"},
	} {
		insertExtraSession(t, database, s.id, s.harness)
	}

	harnesses := []struct {
		claimID   string
		sessionID string
		agent     string
		agentID   string
	}{
		{"clm-ph-claude", "sess-claude-ph", "claude-code", "agent-ph-claude"},
		{"clm-ph-codex", "sess-codex-ph", "codex-cli", "agent-ph-codex"},
		{"clm-ph-gemini", "sess-gemini-ph", "gemini-cli", "agent-ph-gemini"},
	}

	for _, h := range harnesses {
		c := &models.Claim{
			ClaimID:          h.claimID,
			WorkItemID:       "feat-parallel",
			OwnerSessionID:   h.sessionID,
			OwnerAgent:       h.agent,
			ClaimedByAgentID: h.agentID,
			Status:           models.ClaimInProgress,
		}
		if err := db.ClaimItemOrRenew(database, c, 30*time.Minute); err != nil {
			t.Fatalf("claim %s: %v", h.claimID, err)
		}
	}

	coll, err := db.DetectCollaboration(database, "feat-parallel")
	if err != nil {
		t.Fatalf("DetectCollaboration: %v", err)
	}
	if !coll.HasCollision {
		t.Errorf("expected collision with 3 parallel harness claimants")
	}
	if len(coll.Claimants) != 3 {
		t.Errorf("expected 3 claimants, got %d", len(coll.Claimants))
	}

	for _, h := range harnesses {
		found := false
		for _, cl := range coll.Claimants {
			if cl.OwnerSessionID == h.sessionID && cl.OwnerAgent == h.agent {
				found = true
			}
		}
		if !found {
			t.Errorf("harness %s/%s not in collaboration claimants", h.agent, h.sessionID)
		}
	}
}

// TestSubagentAttribution_OutOfOrderParent verifies that subagent session rows
// written BEFORE the parent session record arrives are retained and correctly
// linked after BackfillParentSession is called.
func TestSubagentAttribution_OutOfOrderParent(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	insertExtraFeature(t, database, "feat-ooo")

	// Insert child session WITHOUT parent yet (out-of-order arrival).
	_, err := database.Exec(
		`INSERT OR IGNORE INTO sessions (session_id, agent_assigned, created_at, status, is_subagent)
		 VALUES ('agent-child-ooo', 'feature-coder', ?, 'active', 1)`,
		now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insert child session: %v", err)
	}

	// Insert child event with work-item attribution before parent exists.
	ev := &models.AgentEvent{
		EventID:      "evt-ooo-1",
		AgentID:      "agent-child-ooo",
		EventType:    models.EventToolCall,
		Timestamp:    now,
		ToolName:     "Write",
		InputSummary: "child editing",
		SessionID:    "agent-child-ooo",
		FeatureID:    "feat-ooo",
		Status:       "started",
		Source:       "hook",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := db.InsertEvent(database, ev); err != nil {
		t.Fatalf("insert child event: %v", err)
	}

	// Parent arrives late.
	parent := &models.Session{
		SessionID:     "sess-parent-ooo",
		AgentAssigned: "claude-code",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, parent); err != nil {
		t.Fatalf("insert parent: %v", err)
	}

	// Backfill the parent link on the child session.
	if err := db.BackfillParentSession(database, "agent-child-ooo", "sess-parent-ooo"); err != nil {
		t.Fatalf("BackfillParentSession: %v", err)
	}

	// Child event must still exist with feature attribution.
	retrieved, err := db.GetEvent(database, "evt-ooo-1")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if retrieved == nil {
		t.Fatal("child event not found after parent backfill")
	}
	if retrieved.FeatureID != "feat-ooo" {
		t.Errorf("child event lost feature: got %q, want %q", retrieved.FeatureID, "feat-ooo")
	}

	// Parent row now has a linked child.
	var childSID string
	err = database.QueryRow(
		`SELECT session_id FROM sessions WHERE parent_session_id = ?`, "sess-parent-ooo",
	).Scan(&childSID)
	if err == sql.ErrNoRows {
		t.Error("child session not linked to parent after BackfillParentSession")
	} else if err != nil {
		t.Fatalf("query child: %v", err)
	}
}

// insertExtraFeature inserts a feature row for FK satisfaction (does not fail if exists).
func insertExtraFeature(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := database.Exec(
		`INSERT OR IGNORE INTO features (id, type, title, status, priority, created_at, updated_at)
		 VALUES (?, 'feature', ?, 'in-progress', 'medium', ?, ?)`,
		id, id, now.Format(time.RFC3339), now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insertExtraFeature %s: %v", id, err)
	}
}

// insertExtraSession inserts a minimal session row for FK satisfaction.
func insertExtraSession(t *testing.T, database *sql.DB, sessionID, agent string) {
	t.Helper()
	now := time.Now().UTC()
	_, err := database.Exec(
		`INSERT OR IGNORE INTO sessions (session_id, agent_assigned, created_at, status)
		 VALUES (?, ?, ?, 'active')`,
		sessionID, agent, now.Format(time.RFC3339),
	)
	if err != nil {
		t.Fatalf("insertExtraSession %s: %v", sessionID, err)
	}
}
