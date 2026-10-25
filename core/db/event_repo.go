package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// lookupAgentIDByEvent returns the agent_id of an existing event, or "" if the
// event does not exist. Used to materialise parent_agent_id at insert time
// without failing when the parent row hasn't been written yet (race condition).
func lookupAgentIDByEvent(database *sql.DB, eventID string) string {
	if eventID == "" {
		return ""
	}
	var agentID string
	err := database.QueryRow(
		`SELECT agent_id FROM agent_events WHERE event_id = ?`, eventID,
	).Scan(&agentID)
	if err != nil {
		// parent row not found (race) or other error — leave blank, non-fatal
		return ""
	}
	return agentID
}

// InsertEvent writes an agent event row.
// If ParentEventID is set and ParentAgentID is empty, the parent row's agent_id
// is resolved automatically and stored as parent_agent_id, materialising the
// agent-to-agent lineage edge in a single hop. Only new rows written after this
// change will have parent_agent_id populated; no historical backfill is performed.
//
// parent_event_id is stored as best-effort lineage metadata with no FK constraint
// (removed in bug-89990f33): the row is always persisted even when the parent row
// doesn't exist yet (timing races are now silently OK).
func InsertEvent(db *sql.DB, e *models.AgentEvent) error {
	if e.ParentEventID != "" && e.ParentAgentID == "" {
		e.ParentAgentID = lookupAgentIDByEvent(db, e.ParentEventID)
	}
	_, err := db.Exec(`
		INSERT INTO agent_events (
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
		return fmt.Errorf("insert event %s: %w", e.EventID, err)
	}
	return nil
}

// EventExists returns true when an agent_event row with the given ID exists in
// the database. It is cheaper than GetEvent when the caller only needs to
// validate existence (e.g. to guard against using a stale env-var ID).
func EventExists(database *sql.DB, eventID string) bool {
	if database == nil {
		return false
	}
	var count int
	err := database.QueryRow(
		`SELECT COUNT(1) FROM agent_events WHERE event_id = ?`, eventID,
	).Scan(&count)
	return err == nil && count > 0
}

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

// EnsureSession inserts a minimal sessions row for sessionID if none exists, so
// agent_events (whose session_id FKs to sessions) can be applied even when child
// events arrive before their parent session row (out-of-order async write ops).
func EnsureSession(database *sql.DB, sessionID string) error {
	_, err := database.Exec(
		`INSERT OR IGNORE INTO sessions (session_id, agent_assigned, status) VALUES (?, ?, 'active')`,
		sessionID, "__hook__",
	)
	return err
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

// UpdateEventFields performs a partial UPDATE on an event, setting status,
// output_summary, and updated_at.
func UpdateEventFields(db *sql.DB, eventID, status, outputSummary string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE agent_events
		SET status = ?, output_summary = ?, updated_at = ?
		WHERE event_id = ?`,
		status, nullStr(outputSummary), now, eventID,
	)
	return err
}

// FindStartedDelegation returns the event_id of the most recent started
// task_delegation in the session. Returns ("", sql.ErrNoRows) when not found.
func FindStartedDelegation(db *sql.DB, sessionID string) (string, error) {
	var eventID string
	err := db.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ?
		  AND event_type IN ('task_delegation', 'delegation')
		  AND status = 'started'
		ORDER BY timestamp DESC
		LIMIT 1`, sessionID,
	).Scan(&eventID)
	return eventID, err
}

// FindDelegationByAgent returns the most recent delegation event for the agent
// (any status). Returns ("", sql.ErrNoRows) when not found.
func FindDelegationByAgent(db *sql.DB, sessionID, agentID string) (string, error) {
	if db == nil {
		return "", sql.ErrNoRows
	}
	var eventID string
	err := db.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ?
		  AND event_type IN ('task_delegation', 'delegation')
		  AND agent_id = ?
		ORDER BY timestamp DESC
		LIMIT 1`, sessionID, agentID,
	).Scan(&eventID)
	return eventID, err
}

// FindStartedDelegationByAgent returns the most recent started delegation for
// the agent. Returns ("", sql.ErrNoRows) when not found.
func FindStartedDelegationByAgent(db *sql.DB, sessionID, agentID string) (string, error) {
	var eventID string
	err := db.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ?
		  AND event_type IN ('task_delegation', 'delegation')
		  AND agent_id = ?
		  AND status = 'started'
		ORDER BY timestamp DESC
		LIMIT 1`, sessionID, agentID,
	).Scan(&eventID)
	return eventID, err
}

// LatestEventByTool returns the event_id of the most recent event for the given
// session and tool_name, regardless of status. Returns ("", sql.ErrNoRows) when not found.
func LatestEventByTool(db *sql.DB, sessionID, toolName string) (string, error) {
	if db == nil {
		return "", sql.ErrNoRows
	}
	var eventID string
	err := db.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ? AND tool_name = ?
		ORDER BY timestamp DESC
		LIMIT 1`, sessionID, toolName,
	).Scan(&eventID)
	return eventID, err
}

// CountEventsByTool returns a map of tool_name → count for all non-empty
// tool_name events in the given session, ordered by count DESC.
func CountEventsByTool(db *sql.DB, sessionID string) (map[string]int, error) {
	if db == nil {
		return map[string]int{}, nil
	}
	rows, err := db.Query(`
		SELECT tool_name, COUNT(*) FROM agent_events
		WHERE session_id = ? AND tool_name != ''
		GROUP BY tool_name ORDER BY COUNT(*) DESC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("count events by tool for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var tool string
		var count int
		if err := rows.Scan(&tool, &count); err != nil {
			return nil, err
		}
		counts[tool] = count
	}
	return counts, rows.Err()
}

// DistinctFeatureIDs returns unique non-empty feature_ids recorded in
// agent_events for the given session.
func DistinctFeatureIDs(db *sql.DB, sessionID string) ([]string, error) {
	rows, err := db.Query(`
		SELECT DISTINCT feature_id FROM agent_events
		WHERE session_id = ? AND feature_id != '' AND feature_id IS NOT NULL`,
		sessionID)
	if err != nil {
		return nil, fmt.Errorf("distinct feature ids for session %s: %w", sessionID, err)
	}
	defer rows.Close()

	var feats []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		feats = append(feats, f)
	}
	return feats, rows.Err()
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

// CountRecentDuplicates returns the count of events matching tool_name and
// input_summary within the last windowSeconds. Used for dedup checks.
func CountRecentDuplicates(db *sql.DB, sessionID, toolName, inputSummary string, windowSeconds int) (int, error) {
	if db == nil {
		return 0, nil
	}
	var count int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM agent_events
		 WHERE session_id = ? AND tool_name = ? AND input_summary = ?
		   AND timestamp > datetime('now', ? || ' seconds')`,
		sessionID, toolName, inputSummary, fmt.Sprintf("-%d", windowSeconds),
	).Scan(&count)
	return count, err
}
