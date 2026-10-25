package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// UpsertSession inserts a new session row or upgrades an existing placeholder
// row (created by EnsureSession) with real metadata. Called by the applier's
// OpTypeSessionInsert path so that out-of-order arrival — where an agent_event
// op created a "__hook__" placeholder before the real session.insert arrived —
// results in the final row carrying the real metadata rather than sticking with
// the placeholder values.
//
// The WHERE guard ensures we only upgrade placeholder rows (agent_assigned='__hook__')
// and never overwrite real session rows created by other means (which may have
// progressed state like completed status). A replayed/late session.insert must not
// revert a completed session back to active, for example.
//
// CRITICAL: The upgrade-only branch (DO UPDATE) omits status and completed_at.
// These fields are lifecycle-owned state and must NEVER be reverted by a late/
// replayed session.insert. The upgrade attaches real session identity/metadata
// (agent_assigned, parent_session_id, etc.) to a pristine placeholder; lifecycle
// status is managed exclusively by session.status ops (OpTypeSessionStatus).
func UpsertSession(db *sql.DB, s *models.Session) error {
	_, err := db.Exec(`
		INSERT INTO sessions (session_id, agent_assigned, parent_session_id,
			parent_event_id, created_at, status, start_commit,
			is_subagent, model, active_feature_id, git_remote_url, project_dir,
			exec_worktree_path, branch, harness, continued_from)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			agent_assigned=excluded.agent_assigned,
			parent_session_id=excluded.parent_session_id,
			parent_event_id=excluded.parent_event_id,
			created_at=excluded.created_at,
			start_commit=excluded.start_commit,
			is_subagent=excluded.is_subagent,
			model=excluded.model,
			active_feature_id=excluded.active_feature_id,
			git_remote_url=excluded.git_remote_url,
			project_dir=excluded.project_dir,
			exec_worktree_path=excluded.exec_worktree_path,
			branch=excluded.branch,
			harness=excluded.harness,
			continued_from=excluded.continued_from
		WHERE sessions.agent_assigned = '__hook__'`,
		s.SessionID, s.AgentAssigned, nullStr(s.ParentSessionID),
		nullStr(s.ParentEventID), s.CreatedAt.UTC().Format(time.RFC3339),
		s.Status, nullStr(s.StartCommit),
		s.IsSubagent, nullStr(s.Model), nullStr(s.ActiveFeatureID),
		nullStr(s.GitRemoteURL),
		nullStr(s.ProjectDir),
		nullStr(s.ExecWorktreePath),
		nullStr(s.Branch),
		nullStr(s.Harness),
		nullStr(s.ContinuedFrom),
	)
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", s.SessionID, err)
	}
	return nil
}

// GetSession retrieves a session by ID.
func GetSession(db *sql.DB, sessionID string) (*models.Session, error) {
	if db == nil {
		return nil, sql.ErrNoRows
	}
	row := db.QueryRow(`
		SELECT session_id, agent_assigned, parent_session_id,
			parent_event_id, created_at, completed_at,
			total_events, total_tokens_used, context_drift,
			status, is_subagent, model, active_feature_id, project_dir,
			last_user_query_at, last_user_query, handoff_notes, recommended_next,
			blockers, recommended_context, continued_from,
			exec_worktree_path, branch, harness
		FROM sessions WHERE session_id = ?`, sessionID)

	s := &models.Session{}
	var parentSess, parentEvt, completedAt, model, activeFeat, projectDir sql.NullString
	var lastUserQueryAt, lastUserQuery, handoffNotes, recommendedNext sql.NullString
	var blockers, recommendedContext, continuedFrom sql.NullString
	var execWorktreePath, branch, harness sql.NullString
	var createdStr string

	err := row.Scan(
		&s.SessionID, &s.AgentAssigned, &parentSess,
		&parentEvt, &createdStr, &completedAt,
		&s.TotalEvents, &s.TotalTokensUsed, &s.ContextDrift,
		&s.Status, &s.IsSubagent, &model, &activeFeat, &projectDir,
		&lastUserQueryAt, &lastUserQuery, &handoffNotes, &recommendedNext,
		&blockers, &recommendedContext, &continuedFrom,
		&execWorktreePath, &branch, &harness,
	)
	if err != nil {
		return nil, fmt.Errorf("get session %s: %w", sessionID, err)
	}

	s.ParentSessionID = parentSess.String
	s.ParentEventID = parentEvt.String
	s.Model = model.String
	s.ActiveFeatureID = activeFeat.String
	s.ProjectDir = projectDir.String
	s.LastUserQueryAt = lastUserQueryAt.String
	s.LastUserQuery = lastUserQuery.String
	s.HandoffNotes = handoffNotes.String
	s.RecommendedNext = recommendedNext.String
	if blockers.Valid && blockers.String != "" {
		s.Blockers = json.RawMessage(blockers.String)
	}
	if recommendedContext.Valid && recommendedContext.String != "" {
		s.RecommendedContext = json.RawMessage(recommendedContext.String)
	}
	s.ContinuedFrom = continuedFrom.String
	s.ExecWorktreePath = execWorktreePath.String
	s.Branch = branch.String
	s.Harness = harness.String
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)

	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		s.CompletedAt = &t
	}
	return s, nil
}

// UpdateSessionHandoff writes deterministic SessionEnd handoff fields onto a
// session row without clobbering existing non-empty values with empty input.
// Callers may pass empty strings for any field they do not want to change.
func UpdateSessionHandoff(db *sql.DB, sessionID, handoffNotes, recommendedNext string, blockersJSON, recommendedContextJSON []byte) error {
	if sessionID == "" {
		return nil
	}

	var blockersArg any
	if len(blockersJSON) > 0 {
		blockersArg = string(blockersJSON)
	}
	var recommendedContextArg any
	if len(recommendedContextJSON) > 0 {
		recommendedContextArg = string(recommendedContextJSON)
	}

	_, err := db.Exec(`
		UPDATE sessions
		SET handoff_notes = COALESCE(NULLIF(?, ''), handoff_notes),
		    recommended_next = COALESCE(NULLIF(?, ''), recommended_next),
		    blockers = COALESCE(NULLIF(?, ''), blockers),
		    recommended_context = COALESCE(NULLIF(?, ''), recommended_context)
		WHERE session_id = ?`,
		handoffNotes, recommendedNext, blockersArg, recommendedContextArg, sessionID,
	)
	if err != nil {
		return fmt.Errorf("update session handoff %s: %w", sessionID, err)
	}
	return nil
}

// UpdateSessionStatus sets the status and optionally the completed_at timestamp.
func UpdateSessionStatus(db *sql.DB, sessionID, status string) error {
	var completedAt *string
	if status == "completed" || status == "failed" {
		now := time.Now().UTC().Format(time.RFC3339)
		completedAt = &now
	}
	_, err := db.Exec(`
		UPDATE sessions SET status = ?, completed_at = COALESCE(?, completed_at)
		WHERE session_id = ?`,
		status, completedAt, sessionID,
	)
	return err
}

// ListSessions returns sessions ordered by created_at DESC with an optional
// active-only filter and row limit.
func ListSessions(db *sql.DB, activeOnly bool, limit int) ([]*models.Session, error) {
	if limit <= 0 {
		limit = 10
	}

	query := `
		SELECT session_id, agent_assigned, created_at, completed_at, status, model
		FROM sessions`
	if activeOnly {
		query += " WHERE status = 'active'"
	}
	query += " ORDER BY created_at DESC LIMIT ?"

	rows, err := db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		s := &models.Session{}
		var completedAt, model sql.NullString
		var createdStr string

		if err := rows.Scan(
			&s.SessionID, &s.AgentAssigned, &createdStr,
			&completedAt, &s.Status, &model,
		); err != nil {
			return nil, fmt.Errorf("scan session row: %w", err)
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
		s.Model = model.String
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			s.CompletedAt = &t
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

// MostRecentActiveSession returns the session_id of the latest active session,
// or ("", nil) if none exists.
func MostRecentActiveSession(db *sql.DB) (string, error) {
	row := db.QueryRow(`
		SELECT session_id FROM sessions
		WHERE status = 'active'
		ORDER BY created_at DESC LIMIT 1`)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("most recent active session: %w", err)
	}
	return id, nil
}

// ToolUseContextRow holds the batch-fetched session + claim fields used by
// resolveToolUseContext. Replaces three separate queries (GetSession,
// GetActiveFeatureID, HasActiveClaimByAgent) with a single SQL join.
type ToolUseContextRow struct {
	SessionID       string
	ActiveFeatureID string
	ParentSessionID string
	IsSubagent      bool
	CreatedAt       time.Time
	// ClaimedItem is the work_item_id of the agent's active claim, or "".
	ClaimedItem string
}

// GetToolUseContext fetches the session and active claim for agentID in a
// single query, replacing three separate reads on the PreToolUse hot path.
// Returns nil when the session does not exist.
//
// active_feature_id is only returned when the referenced feature is actually
// in-progress — a stale pointer to a completed feature is treated as empty,
// so guards correctly block edits without an active work item.
func GetToolUseContext(db *sql.DB, sessionID, agentID string) (*ToolUseContextRow, error) {
	// Nil-DB-safe (roborev-478 finding 1): the read-only hook dispatch may
	// invoke the handler with a nil DB so the DB-INDEPENDENT guards still run.
	// Treat a nil handle as "no session row" (nil, nil — same shape as
	// sql.ErrNoRows here) rather than panicking on QueryRow.
	if db == nil {
		return nil, nil
	}
	// Claim lookup uses three paths, tried in order:
	//   1. claimed_by_agent_id = agentID  — the direct per-agent claim
	//   2. owner_session_id   = sessionID — fallback for subagent tool calls,
	//      which share the orchestrator's session_id but carry a distinct
	//      agent_id that never had its own claim row (bug-cb4918d8). The
	//      orchestrator's claim is keyed on owner_session_id, so this resolves
	//      the parent's claim for any subagent running under it.
	//   3. session_family_id sibling — fallback for harnesses that invoke hooks
	//      with a sibling/root session token while CLI attribution landed on
	//      another member of the same logical session family.
	// All claim paths are expressed as correlated subqueries so the outer row
	// remains a single sessions row (LIMIT 1 stays exact) and the primary
	// agent-id match wins over the session-id fallback via COALESCE ordering.
	row := db.QueryRow(`
		SELECT s.session_id,
		       COALESCE(
		         CASE WHEN f.status = 'in-progress' THEN s.active_feature_id ELSE '' END,
		         ''
		       ) AS active_feature_id,
		       COALESCE(s.parent_session_id, '') AS parent_session_id,
		       s.is_subagent,
		       s.created_at,
		       COALESCE(
		         (SELECT c.work_item_id FROM claims c
		           WHERE c.claimed_by_agent_id = ?
		             AND c.owner_session_id = ?
		             AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
		           ORDER BY c.leased_at DESC
		           LIMIT 1),
		         (SELECT c.work_item_id FROM claims c
		           WHERE c.owner_session_id = ?
		             AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
		           ORDER BY c.leased_at DESC
		           LIMIT 1),
		         (SELECT c.work_item_id FROM claims c
		           JOIN sessions cs ON cs.session_id = c.owner_session_id
		           WHERE c.owner_session_id != s.session_id
		             AND cs.status = 'active'
		             AND COALESCE(NULLIF(cs.session_family_id, ''), cs.session_id) = COALESCE(NULLIF(s.session_family_id, ''), s.session_id)
		             AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
		           ORDER BY c.leased_at DESC
		           LIMIT 1),
		         ''
		       ) AS claimed_item
		FROM sessions s
		LEFT JOIN features f ON f.id = s.active_feature_id
		WHERE s.session_id = ?
		LIMIT 1`,
		agentID, sessionID, sessionID, sessionID,
	)

	r := &ToolUseContextRow{}
	var createdStr string
	err := row.Scan(
		&r.SessionID,
		&r.ActiveFeatureID,
		&r.ParentSessionID,
		&r.IsSubagent,
		&createdStr,
		&r.ClaimedItem,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get tool use context %s: %w", sessionID, err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	return r, nil
}

// GetActiveFeatureIDForSession returns the active_feature_id for sessionID, or
// "" when the session has none. Lightweight single-column lookup used by the
// parent-session fallback in autoCompleteFromCommit.
func GetActiveFeatureIDForSession(db *sql.DB, sessionID string) string {
	if db == nil || sessionID == "" {
		return ""
	}
	var id sql.NullString
	db.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&id)
	return id.String
}

// nullStr converts an empty string to sql.NullString.
func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// SessionFile holds a file path and its access metadata for a given session.
type SessionFile struct {
	FilePath  string `json:"file_path"`
	Operation string `json:"operation"`
	LastSeen  string `json:"last_seen"`
}

// ListFilesBySession returns all file paths recorded for the given session,
// reusing the feature_files.session_id column (schema ~301-311, nullable).
// Results are ordered by last_seen DESC. Returns nil (not an error) when the
// session has no recorded files.
func ListFilesBySession(db *sql.DB, sessionID string) ([]SessionFile, error) {
	if sessionID == "" {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT file_path, COALESCE(operation, ''), last_seen
		FROM feature_files
		WHERE session_id = ?
		ORDER BY last_seen DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list files for session %s: %w", sessionID, err)
	}
	defer rows.Close()
	var out []SessionFile
	for rows.Next() {
		var sf SessionFile
		if err := rows.Scan(&sf.FilePath, &sf.Operation, &sf.LastSeen); err != nil {
			continue
		}
		out = append(out, sf)
	}
	return out, rows.Err()
}

// GetSessionsByFamily returns all session_ids that belong to the given family.
// Results are ordered by created_at DESC so the most recent session is first.
func GetSessionsByFamily(db *sql.DB, familyID string) ([]string, error) {
	rows, err := db.Query(
		`SELECT session_id FROM sessions WHERE session_family_id = ? ORDER BY created_at DESC`,
		familyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get sessions by family %s: %w", familyID, err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan session id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ListClaimlessFilesBySession returns claimless touches from the session_files
// ledger for the given session, newest first. Distinct from
// ListFilesBySession (which reads feature_files for *claimed* touches).
func ListClaimlessFilesBySession(db *sql.DB, sessionID string) ([]SessionFile, error) {
	if sessionID == "" {
		return nil, nil
	}
	rows, err := db.Query(`
		SELECT file_path, COALESCE(operation, ''), last_seen
		FROM session_files
		WHERE session_id = ?
		ORDER BY last_seen DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list claimless files for session %s: %w", sessionID, err)
	}
	defer rows.Close()
	var out []SessionFile
	for rows.Next() {
		var sf SessionFile
		if err := rows.Scan(&sf.FilePath, &sf.Operation, &sf.LastSeen); err != nil {
			continue
		}
		out = append(out, sf)
	}
	return out, rows.Err()
}
