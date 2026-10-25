// Package dbtest holds fixture writers for tests that need rows in the SQLite
// read index which no production path writes directly any more (claims are
// projected from the canonical claim ledger). Production code must not import
// it.
package dbtest

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// InsertClaim writes claim as an active row with a lease of leaseDuration,
// creating placeholder features and sessions rows for its foreign keys.
func InsertClaim(database *sql.DB, claim *models.Claim, leaseDuration time.Duration) error {
	now := time.Now().UTC()
	claim.LeasedAt, claim.LastHeartbeatAt = now, now
	claim.CreatedAt, claim.UpdatedAt = now, now
	claim.LeaseExpiresAt = now.Add(leaseDuration)
	if claim.Status == "" {
		claim.Status = models.ClaimProposed
	}
	if claim.OwnerAgent == "" {
		claim.OwnerAgent = "claude-code"
	}
	ensureRows(database, claim, now.Format(time.RFC3339))
	ts := now.Format(time.RFC3339)
	_, err := database.Exec(`
		INSERT INTO claims (
			claim_id, work_item_id, track_id, owner_session_id, owner_agent,
			claimed_by_agent_id, status, intended_output, write_scope,
			leased_at, lease_expires_at, last_heartbeat_at,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		claim.ClaimID, claim.WorkItemID, nullable(claim.TrackID),
		claim.OwnerSessionID, claim.OwnerAgent, claim.ClaimedByAgentID,
		string(claim.Status), nullable(claim.IntendedOutput), nullable(string(claim.WriteScope)),
		ts, claim.LeaseExpiresAt.Format(time.RFC3339), ts, ts, ts,
	)
	if err != nil {
		return fmt.Errorf("insert claim %s: %w", claim.ClaimID, err)
	}
	return nil
}

// HasActiveClaim reports whether workItemID has a claim in an active status.
func HasActiveClaim(database *sql.DB, workItemID string) bool {
	quoted := make([]string, len(models.ActiveClaimStatuses))
	for i, s := range models.ActiveClaimStatuses {
		quoted[i] = "'" + string(s) + "'"
	}
	var n int
	err := database.QueryRow(fmt.Sprintf(
		`SELECT COUNT(*) FROM claims WHERE work_item_id = ? AND status IN (%s)`,
		strings.Join(quoted, ",")), workItemID).Scan(&n)
	return err == nil && n > 0
}

func ensureRows(database *sql.DB, claim *models.Claim, ts string) {
	itemType := "feature"
	switch {
	case strings.HasPrefix(claim.WorkItemID, "bug-"):
		itemType = "bug"
	case strings.HasPrefix(claim.WorkItemID, "spk-"):
		itemType = "spike"
	}
	_, _ = database.Exec(`
		INSERT OR IGNORE INTO features (id, type, title, status, priority, created_at, updated_at)
		VALUES (?, ?, '', 'in-progress', 'medium', ?, ?)`,
		claim.WorkItemID, itemType, ts, ts)
	_, _ = database.Exec(`
		INSERT OR IGNORE INTO sessions (session_id, agent_assigned, status, created_at)
		VALUES (?, ?, 'active', ?)`,
		claim.OwnerSessionID, claim.OwnerAgent, ts)
}

func nullable(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}
