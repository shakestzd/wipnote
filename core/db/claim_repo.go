package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// activeStatusList returns the active claim statuses as a SQL IN-list literal.
// Example: "'proposed','claimed','in_progress','blocked','handoff_pending'"
func activeStatusList() string {
	quoted := make([]string, len(models.ActiveClaimStatuses))
	for i, s := range models.ActiveClaimStatuses {
		quoted[i] = "'" + string(s) + "'"
	}
	return strings.Join(quoted, ",")
}

// HeartbeatClaim renews the lease on an active claim owned by sessionID.
// Updates last_heartbeat_at and extends lease_expires_at by leaseDuration.
func HeartbeatClaim(db *sql.DB, claimID, sessionID string, leaseDuration time.Duration) error {
	now := time.Now().UTC()
	newExpiry := now.Add(leaseDuration)
	activeList := activeStatusList()

	query := fmt.Sprintf(`
		UPDATE claims
		SET last_heartbeat_at = ?, lease_expires_at = ?, updated_at = ?
		WHERE claim_id = ?
		  AND owner_session_id = ?
		  AND status IN (%s)`, activeList)

	result, err := db.Exec(query,
		now.Format(time.RFC3339), newExpiry.Format(time.RFC3339),
		now.Format(time.RFC3339), claimID, sessionID,
	)
	if err != nil {
		return fmt.Errorf("heartbeat claim %s: %w", claimID, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("claim %s not found, not owned by session %s, or not active", claimID, sessionID)
	}
	return nil
}

// ReleaseClaim sets a claim to completed or abandoned.
// terminalStatus must be ClaimCompleted or ClaimAbandoned.
func ReleaseClaim(db *sql.DB, claimID, sessionID string, terminalStatus models.ClaimStatus) error {
	if terminalStatus != models.ClaimCompleted && terminalStatus != models.ClaimAbandoned {
		return fmt.Errorf("terminalStatus must be completed or abandoned, got %s", terminalStatus)
	}
	now := time.Now().UTC()
	result, err := db.Exec(`
		UPDATE claims SET status = ?, updated_at = ?
		WHERE claim_id = ? AND owner_session_id = ?`,
		string(terminalStatus), now.Format(time.RFC3339), claimID, sessionID,
	)
	if err != nil {
		return fmt.Errorf("release claim %s: %w", claimID, err)
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("claim %s not found or not owned by session %s", claimID, sessionID)
	}
	return nil
}

// GetClaim returns a claim by ID.
func GetClaim(db *sql.DB, claimID string) (*models.Claim, error) {
	row := db.QueryRow(`
		SELECT claim_id, work_item_id, track_id, owner_session_id, owner_agent,
		       claimed_by_agent_id,
		       status, intended_output, write_scope,
		       leased_at, lease_expires_at, last_heartbeat_at,
		       dependencies, progress_notes, blocker_reason,
		       created_at, updated_at
		FROM claims WHERE claim_id = ?`, claimID)

	c, err := scanClaim(row)
	if err != nil {
		return nil, fmt.Errorf("get claim %s: %w", claimID, err)
	}
	return c, nil
}

// ListActiveClaimsBySession returns all active claims for a session.
func ListActiveClaimsBySession(db *sql.DB, sessionID string) ([]models.Claim, error) {
	activeList := activeStatusList()
	query := fmt.Sprintf(`
		SELECT claim_id, work_item_id, track_id, owner_session_id, owner_agent,
		       claimed_by_agent_id,
		       status, intended_output, write_scope,
		       leased_at, lease_expires_at, last_heartbeat_at,
		       dependencies, progress_notes, blocker_reason,
		       created_at, updated_at
		FROM claims
		WHERE owner_session_id = ? AND status IN (%s)
		ORDER BY created_at DESC`, activeList)

	return queryClaims(db, query, sessionID)
}

// ListClaims returns claims matching the given filters.
// If sessionID is empty, returns all. If statusFilter is empty, returns all statuses.
func ListClaims(db *sql.DB, sessionID, statusFilter string, limit int) ([]models.Claim, error) {
	if limit <= 0 {
		limit = 100
	}

	var conditions []string
	var args []any

	if sessionID != "" {
		conditions = append(conditions, "owner_session_id = ?")
		args = append(args, sessionID)
	}
	if statusFilter != "" {
		conditions = append(conditions, "status = ?")
		args = append(args, statusFilter)
	}

	query := `
		SELECT claim_id, work_item_id, track_id, owner_session_id, owner_agent,
		       claimed_by_agent_id,
		       status, intended_output, write_scope,
		       leased_at, lease_expires_at, last_heartbeat_at,
		       dependencies, progress_notes, blocker_reason,
		       created_at, updated_at
		FROM claims`
	if len(conditions) > 0 {
		query += " WHERE " + strings.Join(conditions, " AND ")
	}
	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	return queryClaims(db, query, args...)
}

// scanClaim scans a single claim row from a *sql.Row.
// The SELECT must include claimed_by_agent_id between owner_agent and status.
func scanClaim(row *sql.Row) (*models.Claim, error) {
	c := &models.Claim{}
	var (
		trackID, intendedOutput, progressNotes, blockerReason sql.NullString
		writeScope, dependencies                              sql.NullString
		leasedStr, expiresStr, heartbeatStr                   string
		createdStr, updatedStr                                string
	)
	err := row.Scan(
		&c.ClaimID, &c.WorkItemID, &trackID, &c.OwnerSessionID, &c.OwnerAgent,
		&c.ClaimedByAgentID,
		&c.Status, &intendedOutput, &writeScope,
		&leasedStr, &expiresStr, &heartbeatStr,
		&dependencies, &progressNotes, &blockerReason,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return nil, err
	}
	c.TrackID = trackID.String
	c.IntendedOutput = intendedOutput.String
	c.ProgressNotes = progressNotes.String
	c.BlockerReason = blockerReason.String
	if writeScope.Valid && writeScope.String != "" {
		c.WriteScope = json.RawMessage(writeScope.String)
	}
	if dependencies.Valid && dependencies.String != "" {
		c.Dependencies = json.RawMessage(dependencies.String)
	}
	c.LeasedAt, _ = time.Parse(time.RFC3339, leasedStr)
	c.LeaseExpiresAt, _ = time.Parse(time.RFC3339, expiresStr)
	c.LastHeartbeatAt, _ = time.Parse(time.RFC3339, heartbeatStr)
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return c, nil
}

// scanClaimRow scans a claim from a *sql.Rows (multi-row cursor).
// The SELECT must include claimed_by_agent_id between owner_agent and status.
func scanClaimRow(rows *sql.Rows) (models.Claim, error) {
	c := models.Claim{}
	var (
		trackID, intendedOutput, progressNotes, blockerReason sql.NullString
		writeScope, dependencies                              sql.NullString
		leasedStr, expiresStr, heartbeatStr                   string
		createdStr, updatedStr                                string
	)
	err := rows.Scan(
		&c.ClaimID, &c.WorkItemID, &trackID, &c.OwnerSessionID, &c.OwnerAgent,
		&c.ClaimedByAgentID,
		&c.Status, &intendedOutput, &writeScope,
		&leasedStr, &expiresStr, &heartbeatStr,
		&dependencies, &progressNotes, &blockerReason,
		&createdStr, &updatedStr,
	)
	if err != nil {
		return c, err
	}
	c.TrackID = trackID.String
	c.IntendedOutput = intendedOutput.String
	c.ProgressNotes = progressNotes.String
	c.BlockerReason = blockerReason.String
	if writeScope.Valid && writeScope.String != "" {
		c.WriteScope = json.RawMessage(writeScope.String)
	}
	if dependencies.Valid && dependencies.String != "" {
		c.Dependencies = json.RawMessage(dependencies.String)
	}
	c.LeasedAt, _ = time.Parse(time.RFC3339, leasedStr)
	c.LeaseExpiresAt, _ = time.Parse(time.RFC3339, expiresStr)
	c.LastHeartbeatAt, _ = time.Parse(time.RFC3339, heartbeatStr)
	c.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	c.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	return c, nil
}

// queryClaims executes a query and returns a slice of Claim.
func queryClaims(db *sql.DB, query string, args ...any) ([]models.Claim, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("query claims: %w", err)
	}
	defer rows.Close()

	var claims []models.Claim
	for rows.Next() {
		c, err := scanClaimRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claim row: %w", err)
		}
		claims = append(claims, c)
	}
	return claims, rows.Err()
}

// LedgerClaimIDPrefix marks claims rows projected from the canonical claim
// ledger by ProjectClaimsFromEpisodes; the rest of the id is the episode id.
const LedgerClaimIDPrefix = "clm-ledger-"

// ledgerNoHeartbeat and ledgerNoLeaseExpiry fill the NOT NULL heartbeat and
// lease columns of a ledger-projected claim. The ledger records neither, and
// started_at must not stand in for a heartbeat: every heartbeat reader
// (SessionLivenessByHeartbeat, the reaper, reconcile's live predicate) has to
// keep seeing such a session as not heartbeating, exactly as it did when the
// table was empty. That includes LiveCollision, so `wipnote continue` still
// resumes the transcript of a session that crashed without recording its end.
// The ledger liveness rule (root session not ended) is applied only by the
// `feature start` gate, which reads the canonical ledgers directly.
const (
	ledgerNoHeartbeat   = "1970-01-01T00:00:00Z"
	ledgerNoLeaseExpiry = "9999-12-31T23:59:59Z"
)

// ProjectClaimsFromEpisodes hydrates the claims table from the open episodes
// in claim_episodes (bug-ec1ff126). Before this, nothing populated claims in
// a projection, so every claims reader — live-collision detection, collision
// warnings, `wipnote who` — saw zero rows.
//
// It must run after claim_episodes and the sessions ledger are projected:
// claims has foreign keys to sessions and features, so an episode whose
// session or work item is absent from the projection is skipped rather than
// failing the whole pass. Previously projected ledger claims are replaced;
// directly written claims are left alone.
func ProjectClaimsFromEpisodes(database *sql.DB) (int64, error) {
	if _, err := database.Exec(
		`DELETE FROM claims WHERE claim_id LIKE ? || '%'`, LedgerClaimIDPrefix,
	); err != nil {
		return 0, fmt.Errorf("purge ledger claims: %w", err)
	}
	res, err := database.Exec(`
		INSERT OR IGNORE INTO claims (
			claim_id, work_item_id, owner_session_id, owner_agent,
			claimed_by_agent_id, status, leased_at, lease_expires_at,
			last_heartbeat_at, created_at, updated_at
		)
		SELECT ? || ce.episode_id, ce.work_item_id, ce.session_id,
		       COALESCE(NULLIF(s.agent_assigned, ''), 'claude-code'),
		       ce.agent_id, ?, ce.started_at, ?,
		       ?, ce.started_at, ce.started_at
		FROM claim_episodes ce
		JOIN sessions s ON s.session_id = ce.session_id
		WHERE ce.ended_at = ''
		  AND EXISTS (SELECT 1 FROM features f WHERE f.id = ce.work_item_id)`,
		LedgerClaimIDPrefix, string(models.ClaimInProgress), ledgerNoLeaseExpiry, ledgerNoHeartbeat,
	)
	if err != nil {
		return 0, fmt.Errorf("project ledger claims: %w", err)
	}
	return res.RowsAffected()
}
