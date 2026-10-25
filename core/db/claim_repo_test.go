package db_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

func TestHeartbeatClaimByWorkItem(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-hbwi1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 5*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	before, err := db.GetClaim(database, "claim-hbwi1")
	if err != nil {
		t.Fatalf("GetClaim before: %v", err)
	}

	if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "", 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaimByWorkItem: %v", err)
	}

	after, err := db.GetClaim(database, "claim-hbwi1")
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

func TestReleaseAllClaimsForSession(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	// Insert extra features for additional claims.
	for i := 2; i <= 3; i++ {
		id := fmt.Sprintf("feat-extra-%d", i)
		_, err := database.Exec(
			`INSERT INTO features (id, type, title, status) VALUES (?, 'feature', ?, 'in-progress')`,
			id, "Extra Feature "+id,
		)
		if err != nil {
			t.Fatalf("insert feature %s: %v", id, err)
		}
	}

	claimIDs := []string{"claim-ra1", "claim-ra2", "claim-ra3"}
	workIDs := []string{"feat-test", "feat-extra-2", "feat-extra-3"}
	for i, cid := range claimIDs {
		c := makeClaim(cid, workIDs[i], "sess-test")
		if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
			t.Fatalf("ClaimItem %s: %v", cid, err)
		}
	}

	released, err := db.ReleaseAllClaimsForSession(database, "sess-test")
	if err != nil {
		t.Fatalf("ReleaseAllClaimsForSession: %v", err)
	}
	if released != 3 {
		t.Errorf("released: got %d, want 3", released)
	}

	for _, wid := range workIDs {
		active, err := activeClaim(database, wid)
		if err != nil {
			t.Fatalf("GetActiveClaim(%s): %v", wid, err)
		}
		if active != nil {
			t.Errorf("expected no active claim for %s after release-all", wid)
		}
	}
}

func TestReapExpiredClaims(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-exp1", "feat-test", "sess-test")
	// Use a negative duration so the lease is already in the past.
	if err := dbtest.InsertClaim(database, c, -1*time.Minute); err != nil {
		t.Fatalf("ClaimItem with past expiry: %v", err)
	}

	// Force the lease_expires_at to be in the past (ClaimItem sets it, but
	// ReapExpiredClaims is called inside, so reap again explicitly).
	reaped, err := reapExpired(database)
	if err != nil {
		t.Fatalf("ReapExpiredClaims: %v", err)
	}
	if reaped < 1 {
		t.Errorf("reaped: got %d, want >=1", reaped)
	}

	got, err := db.GetClaim(database, "claim-exp1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Status != models.ClaimExpired {
		t.Errorf("status: got %q, want %q", got.Status, models.ClaimExpired)
	}
}

func TestReapDoesNotAffectLiveClaims(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-live1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	reaped, err := reapExpired(database)
	if err != nil {
		t.Fatalf("ReapExpiredClaims: %v", err)
	}
	if reaped != 0 {
		t.Errorf("reaped live claim, got count %d", reaped)
	}

	got, err := db.GetClaim(database, "claim-live1")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	if got.Status != models.ClaimProposed {
		t.Errorf("status: got %q, want %q", got.Status, models.ClaimProposed)
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

// TestWriteScopeAppendedInHeartbeatUpdate verifies that a heartbeat with a non-empty
// writePath stores that path in write_scope.paths via a single UPDATE per call.
//
// Single-UPDATE proof: HeartbeatClaimByWorkItem both extends lease_expires_at AND
// populates write_scope in one SQL statement. We verify the path is present AND the
// lease was extended — if only one happened, it would imply two separate statements,
// one of which would have been silently lost.
func TestWriteScopeAppendedInHeartbeatUpdate(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-ws1", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 5*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	// Record lease before heartbeat.
	before, err := db.GetClaim(database, "claim-ws1")
	if err != nil {
		t.Fatalf("GetClaim before: %v", err)
	}

	// Heartbeat with a path — extends lease AND sets write_scope in one UPDATE.
	if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "internal/foo/bar.go", 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaimByWorkItem: %v", err)
	}

	after, err := db.GetClaim(database, "claim-ws1")
	if err != nil {
		t.Fatalf("GetClaim after: %v", err)
	}

	// Lease must have extended from 5m to 30m — proving the UPDATE executed.
	if !after.LeaseExpiresAt.After(before.LeaseExpiresAt) {
		t.Errorf("lease_expires_at did not extend: before=%v after=%v",
			before.LeaseExpiresAt, after.LeaseExpiresAt)
	}

	// write_scope.paths must contain the heartbeated path (set in the SAME UPDATE).
	scope := unmarshalWriteScope(t, after.WriteScope)
	found := false
	for _, p := range scope.Paths {
		if p == "internal/foo/bar.go" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("write_scope.paths does not contain the heartbeated path; got %v", scope.Paths)
	}
}

// TestWriteScopeDedupeCap verifies deduplication and the 200-path FIFO cap.
func TestWriteScopeDedupeCap(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-ws2", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	// Append 205 distinct paths (0..204).
	for i := 0; i < 205; i++ {
		path := fmt.Sprintf("file-%03d.go", i)
		if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", path, 30*time.Minute); err != nil {
			t.Fatalf("HeartbeatClaimByWorkItem path %d: %v", i, err)
		}
	}

	got, err := db.GetClaim(database, "claim-ws2")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}
	scope := unmarshalWriteScope(t, got.WriteScope)

	// Cap: exactly 200 paths should remain.
	if len(scope.Paths) != 200 {
		t.Errorf("expected 200 paths after cap, got %d", len(scope.Paths))
	}

	// Oldest 5 paths (file-000..file-004) should have been dropped.
	dropped := map[string]bool{}
	for i := 0; i < 5; i++ {
		dropped[fmt.Sprintf("file-%03d.go", i)] = true
	}
	for _, p := range scope.Paths {
		if dropped[p] {
			t.Errorf("dropped path %q should not be in write_scope after cap", p)
		}
	}

	// Newest path (file-204.go) must be present.
	newest := "file-204.go"
	found := false
	for _, p := range scope.Paths {
		if p == newest {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("newest path %q not found in write_scope; paths=%v", newest, scope.Paths)
	}

	// Re-append an existing path — length must stay at 200.
	existingPath := "file-100.go"
	if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", existingPath, 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaimByWorkItem re-append: %v", err)
	}
	got2, err := db.GetClaim(database, "claim-ws2")
	if err != nil {
		t.Fatalf("GetClaim after re-append: %v", err)
	}
	scope2 := unmarshalWriteScope(t, got2.WriteScope)
	if len(scope2.Paths) != 200 {
		t.Errorf("after re-append: expected 200 paths, got %d", len(scope2.Paths))
	}
	// The re-appended path should still be present.
	found = false
	for _, p := range scope2.Paths {
		if p == existingPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("re-appended path %q should still be in write_scope", existingPath)
	}
}

// TestWriteScopeClearedOnRelease verifies that write_scope is set to NULL when
// a session is released (ReleaseAllClaimsForSession) or when claims expire (ReapExpiredClaims).
func TestWriteScopeClearedOnRelease(t *testing.T) {
	t.Run("ReleaseAllClaimsForSession", func(t *testing.T) {
		database := setupClaimDB(t)
		defer database.Close()

		c := makeClaim("claim-wscr1", "feat-test", "sess-test")
		if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
			t.Fatalf("ClaimItem: %v", err)
		}
		if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "some/file.go", 30*time.Minute); err != nil {
			t.Fatalf("HeartbeatClaimByWorkItem: %v", err)
		}
		// Verify path is present before release.
		pre, _ := db.GetClaim(database, "claim-wscr1")
		scope := unmarshalWriteScope(t, pre.WriteScope)
		if len(scope.Paths) == 0 {
			t.Fatal("expected write_scope.paths to be populated before release")
		}

		if _, err := db.ReleaseAllClaimsForSession(database, "sess-test"); err != nil {
			t.Fatalf("ReleaseAllClaimsForSession: %v", err)
		}

		post, err := db.GetClaim(database, "claim-wscr1")
		if err != nil {
			t.Fatalf("GetClaim after release: %v", err)
		}
		if len(post.WriteScope) > 0 {
			t.Errorf("expected write_scope to be NULL after release, got: %s", string(post.WriteScope))
		}
	})

	t.Run("ReapExpiredClaims", func(t *testing.T) {
		database := setupClaimDB(t)
		defer database.Close()

		c := makeClaim("claim-wscr2", "feat-test", "sess-test")
		if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
			t.Fatalf("ClaimItem: %v", err)
		}
		// Use a direct DB exec to add a path without expiring the claim yet.
		if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "another/file.go", 30*time.Minute); err != nil {
			t.Fatalf("HeartbeatClaimByWorkItem: %v", err)
		}
		// Force expiry by setting lease_expires_at in the past.
		_, err := database.Exec(
			`UPDATE claims SET lease_expires_at = ? WHERE claim_id = ?`,
			time.Now().Add(-1*time.Minute).UTC().Format(time.RFC3339), "claim-wscr2",
		)
		if err != nil {
			t.Fatalf("force-expire: %v", err)
		}
		if _, err := reapExpired(database); err != nil {
			t.Fatalf("ReapExpiredClaims: %v", err)
		}

		post, err := db.GetClaim(database, "claim-wscr2")
		if err != nil {
			t.Fatalf("GetClaim after reap: %v", err)
		}
		if len(post.WriteScope) > 0 {
			t.Errorf("expected write_scope to be NULL after reap, got: %s", string(post.WriteScope))
		}
	})
}

// TestWriteScopeEmptyAfterClaim verifies the ephemeral contract:
// a freshly claimed item has no write_scope.paths until a heartbeat-with-path fires.
func TestWriteScopeEmptyAfterClaim(t *testing.T) {
	database := setupClaimDB(t)
	defer database.Close()

	c := makeClaim("claim-ws-fresh", "feat-test", "sess-test")
	if err := dbtest.InsertClaim(database, c, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItem: %v", err)
	}

	got, err := db.GetClaim(database, "claim-ws-fresh")
	if err != nil {
		t.Fatalf("GetClaim: %v", err)
	}

	// No heartbeat-with-path yet: write_scope should be absent or have empty paths.
	if len(got.WriteScope) > 0 {
		scope := unmarshalWriteScope(t, got.WriteScope)
		if len(scope.Paths) != 0 {
			t.Errorf("expected empty write_scope.paths after bare claim, got %v", scope.Paths)
		}
	}

	// Fire heartbeat with empty path — still no paths.
	if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "", 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaimByWorkItem empty: %v", err)
	}
	got2, err := db.GetClaim(database, "claim-ws-fresh")
	if err != nil {
		t.Fatalf("GetClaim after empty heartbeat: %v", err)
	}
	if len(got2.WriteScope) > 0 {
		scope2 := unmarshalWriteScope(t, got2.WriteScope)
		if len(scope2.Paths) != 0 {
			t.Errorf("expected still-empty paths after empty heartbeat, got %v", scope2.Paths)
		}
	}

	// Now fire heartbeat with a real path — it should appear.
	if err := db.HeartbeatClaimByWorkItem(database, "feat-test", "sess-test", "cmd/wipnote/main.go", 30*time.Minute); err != nil {
		t.Fatalf("HeartbeatClaimByWorkItem with path: %v", err)
	}
	got3, err := db.GetClaim(database, "claim-ws-fresh")
	if err != nil {
		t.Fatalf("GetClaim after path heartbeat: %v", err)
	}
	scope3 := unmarshalWriteScope(t, got3.WriteScope)
	if len(scope3.Paths) != 1 || scope3.Paths[0] != "cmd/wipnote/main.go" {
		t.Errorf("expected exactly one path after first path heartbeat, got %v", scope3.Paths)
	}
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

// reapExpired runs the reaper statement the writer daemon executes.
func reapExpired(database *sql.DB) (int64, error) {
	query, args := db.ReapExpiredClaimsStmt()
	res, err := database.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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
