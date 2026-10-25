package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// ListEventsBySessionAsc returns events for a session ordered by timestamp ASC
// including parent_event_id for hierarchy reconstruction.
func ListEventsBySessionAsc(db *sql.DB, sessionID string, limit int) ([]models.AgentEvent, error) {
	if limit <= 0 {
		limit = 500
	}
	rows, err := db.Query(`
		SELECT event_id, agent_id, event_type, timestamp, tool_name,
			session_id, feature_id, parent_event_id, status, model
		FROM agent_events
		WHERE session_id = ?
		ORDER BY timestamp ASC
		LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list events asc for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var events []models.AgentEvent
	for rows.Next() {
		var e models.AgentEvent
		var tsStr string
		var toolName, featID, parentEvt, model sql.NullString

		if err := rows.Scan(
			&e.EventID, &e.AgentID, &e.EventType, &tsStr, &toolName,
			&e.SessionID, &featID, &parentEvt, &e.Status, &model,
		); err != nil {
			return nil, err
		}
		e.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
		e.ToolName = toolName.String
		e.FeatureID = featID.String
		e.ParentEventID = parentEvt.String
		e.Model = model.String
		events = append(events, e)
	}
	return events, rows.Err()
}

// MostRecentSession returns the session_id of the latest session (any status),
// or ("", nil) if the table is empty.
func MostRecentSession(db *sql.DB) (string, error) {
	row := db.QueryRow(`
		SELECT session_id FROM sessions
		ORDER BY created_at DESC LIMIT 1`)
	var id string
	if err := row.Scan(&id); err != nil {
		if err == sql.ErrNoRows {
			return "", nil
		}
		return "", fmt.Errorf("most recent session: %w", err)
	}
	return id, nil
}

// UpsertEvent performs an INSERT OR REPLACE for idempotent event writes.
// This is useful when a hook may fire multiple times for the same logical event.
func UpsertEvent(db *sql.DB, e *models.AgentEvent) error {
	_, err := db.Exec(`
		INSERT OR REPLACE INTO agent_events (
			event_id, agent_id, event_type, timestamp, tool_name,
			input_summary, tool_input, output_summary, session_id, feature_id,
			parent_agent_id, parent_event_id, subagent_type,
			cost_tokens, execution_duration_seconds, status,
			model, claude_task_id, source, step_id,
			created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?)`,
		e.EventID, e.AgentID, string(e.EventType),
		e.Timestamp.UTC().Format(time.RFC3339), nullStr(e.ToolName),
		nullStr(e.InputSummary), nullStr(e.ToolInput), nullStr(e.OutputSummary),
		e.SessionID, nullStr(e.FeatureID),
		nullStr(e.ParentAgentID), nullStr(e.ParentEventID),
		nullStr(e.SubagentType),
		e.CostTokens, e.ExecDuration, e.Status,
		nullStr(e.Model), nullStr(e.ClaudeTaskID),
		e.Source, nullStr(e.StepID),
		e.CreatedAt.UTC().Format(time.RFC3339),
		e.UpdatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("upsert event %s: %w", e.EventID, err)
	}
	return nil
}

// HasHookEventAt reports whether a hook-written event (source != 'ingest')
// already exists for the given session, tool_name, and timestamp (compared at
// second precision). Used by the ingest path to avoid creating duplicate events
// when hooks already recorded the same logical event with a different ID.
func HasHookEventAt(db *sql.DB, sessionID, toolName, timestamp string) (bool, error) {
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM agent_events
		 WHERE session_id = ?
		   AND tool_name = ?
		   AND strftime('%Y-%m-%dT%H:%M:%S', timestamp) = strftime('%Y-%m-%dT%H:%M:%S', ?)
		   AND source != 'ingest'`,
		sessionID, toolName, timestamp,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("has hook event: %w", err)
	}
	return count > 0, nil
}

// DeleteSessionIngestEvents removes agent_events with source='ingest' for a
// session. Hook-written events (source != 'ingest') are preserved so that a
// force re-ingest does not destroy live hook data.
func DeleteSessionIngestEvents(db *sql.DB, sessionID string) error {
	_, err := db.Exec(
		`DELETE FROM agent_events WHERE session_id = ? AND source = 'ingest'`,
		sessionID,
	)
	if err != nil {
		return fmt.Errorf("delete ingest events for %s: %w", sessionID, err)
	}
	return nil
}

// SetPromptID finds the closest UserQuery event in the given session within ±5 s
// of ts and sets its prompt_id column to promptID. It is idempotent: only rows
// with a NULL prompt_id are updated, so re-indexing a session never overwrites an
// existing correlation. If no matching row is found the call is a no-op (returns nil).
func SetPromptID(database *sql.DB, sessionID, promptID string, ts time.Time) error {
	if sessionID == "" || promptID == "" {
		return nil
	}
	lo := ts.UTC().Add(-5 * time.Second).Format(time.RFC3339)
	hi := ts.UTC().Add(5 * time.Second).Format(time.RFC3339)

	var eventID string
	err := database.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ?
		  AND tool_name = 'UserQuery'
		  AND timestamp >= ?
		  AND timestamp <= ?
		  AND prompt_id IS NULL
		ORDER BY ABS(CAST(strftime('%s', timestamp) AS INTEGER) - CAST(strftime('%s', ?) AS INTEGER)) ASC
		LIMIT 1`,
		sessionID, lo, hi, ts.UTC().Format(time.RFC3339),
	).Scan(&eventID)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("set prompt_id lookup: %w", err)
	}

	_, err = database.Exec(
		`UPDATE agent_events SET prompt_id = ? WHERE event_id = ? AND prompt_id IS NULL`,
		promptID, eventID,
	)
	if err != nil {
		return fmt.Errorf("set prompt_id update: %w", err)
	}
	return nil
}
