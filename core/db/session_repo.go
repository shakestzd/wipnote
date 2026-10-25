package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

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
