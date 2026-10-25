package dbtest

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// SetSessionFamilyID sets session_family_id for sessionID; an empty familyID
// means the session is its own family.
func SetSessionFamilyID(database *sql.DB, sessionID, familyID string) error {
	if familyID == "" {
		familyID = sessionID
	}
	_, err := database.Exec(
		`UPDATE sessions SET session_family_id = ? WHERE session_id = ?`, familyID, sessionID)
	return err
}

// InsertLineageTrace writes one agent_lineage_trace row.
func InsertLineageTrace(database *sql.DB, trace *models.LineageTrace) error {
	pathJSON, err := json.Marshal(trace.Path)
	if err != nil {
		return fmt.Errorf("marshal lineage path: %w", err)
	}
	_, err = database.Exec(`
		INSERT INTO agent_lineage_trace
			(trace_id, root_session_id, session_id, agent_name, depth, path,
			 feature_id, started_at, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.TraceID, trace.RootSessionID, nullable(trace.SessionID),
		nullable(trace.AgentName), trace.Depth, string(pathJSON),
		nullable(trace.FeatureID), trace.StartedAt.UTC().Format(time.RFC3339), trace.Status)
	if err != nil {
		return fmt.Errorf("insert lineage trace %s: %w", trace.TraceID, err)
	}
	return nil
}

// GetEvent reads one agent_events row by id.
func GetEvent(database *sql.DB, eventID string) (*models.AgentEvent, error) {
	row := database.QueryRow(`
		SELECT event_id, agent_id, event_type, timestamp, tool_name,
			input_summary, tool_input, output_summary, session_id, feature_id,
			parent_agent_id, parent_event_id, subagent_type,
			cost_tokens, execution_duration_seconds, status,
			model, source, step_id, created_at, updated_at
		FROM agent_events WHERE event_id = ?`, eventID)
	e := &models.AgentEvent{}
	var tsStr, createdStr, updatedStr string
	var toolName, inSum, toolInput, outSum, featID sql.NullString
	var parentAgent, parentEvt, subType, model, src, stepID sql.NullString
	if err := row.Scan(
		&e.EventID, &e.AgentID, &e.EventType, &tsStr, &toolName,
		&inSum, &toolInput, &outSum, &e.SessionID, &featID,
		&parentAgent, &parentEvt, &subType,
		&e.CostTokens, &e.ExecDuration, &e.Status,
		&model, &src, &stepID, &createdStr, &updatedStr,
	); err != nil {
		return nil, fmt.Errorf("get event %s: %w", eventID, err)
	}
	e.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
	e.CreatedAt, _ = time.Parse(time.RFC3339, createdStr)
	e.UpdatedAt, _ = time.Parse(time.RFC3339, updatedStr)
	e.ToolName, e.InputSummary, e.ToolInput = toolName.String, inSum.String, toolInput.String
	e.OutputSummary, e.FeatureID = outSum.String, featID.String
	e.ParentAgentID, e.ParentEventID, e.SubagentType = parentAgent.String, parentEvt.String, subType.String
	e.Model, e.Source, e.StepID = model.String, src.String, stepID.String
	return e, nil
}

// GetPendingSubagentStart reads the pending_subagent_starts row for agentID,
// or nil when there is none.
func GetPendingSubagentStart(database *sql.DB, agentID string) (*db.PendingSubagentStart, error) {
	var p db.PendingSubagentStart
	var cwd, parentAgentID sql.NullString
	err := database.QueryRow(`
		SELECT agent_id, agent_type, session_id, cwd, parent_agent_id, created_at
		FROM pending_subagent_starts WHERE agent_id = ?`, agentID).Scan(
		&p.AgentID, &p.AgentType, &p.SessionID, &cwd, &parentAgentID, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CWD, p.ParentAgentID = cwd.String, parentAgentID.String
	return &p, nil
}
