package db

import (
	"database/sql"
)

// AgentRootSentinel is stored as agent_id when WIPNOTE_AGENT_ID is unset,
// representing the top-level (non-subagent) session owner.
const AgentRootSentinel = "__root__"

// NormaliseAgentID returns agentID if non-empty, otherwise AgentRootSentinel.
// Use this before every call to GetActiveWorkItem.
func NormaliseAgentID(agentID string) string {
	if agentID == "" {
		return AgentRootSentinel
	}
	return agentID
}

// GetActiveWorkItem returns the work item currently claimed by (sessionID, agentID),
// or empty string if no claim exists.
func GetActiveWorkItem(db *sql.DB, sessionID, agentID string) string {
	if db == nil || sessionID == "" {
		return ""
	}
	var id sql.NullString
	db.QueryRow(
		`SELECT work_item_id FROM active_work_items WHERE session_id = ? AND agent_id = ?`,
		sessionID, agentID,
	).Scan(&id)
	return id.String
}

// GetActiveWorkItemWithFallback returns the active work item for (sessionID, agentID),
// falling back to the legacy sessions.active_feature_id column if no row exists
// in active_work_items. This provides compatibility during the transition period
// before active_feature_id is retired.
func GetActiveWorkItemWithFallback(db *sql.DB, sessionID, agentID string) string {
	if db == nil || sessionID == "" {
		return ""
	}
	// Prefer new per-agent table.
	if id := GetActiveWorkItem(db, sessionID, agentID); id != "" {
		return id
	}
	// Fallback to legacy column.
	var id sql.NullString
	db.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&id)
	return id.String
}
