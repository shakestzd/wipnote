package dbtest

import (
	"database/sql"
	"fmt"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// Row fixtures for tests. Production no longer writes these rows through
// core/db (feat-726637b8); tests that seed a database for live readers use
// these instead.

// InsertEvent writes one agent_events row.
func InsertEvent(database *sql.DB, e *models.AgentEvent) error {
	_, err := database.Exec(`
		INSERT INTO agent_events (
			event_id, agent_id, event_type, timestamp, tool_name,
			input_summary, tool_input, output_summary, session_id, feature_id,
			parent_agent_id, parent_event_id, subagent_type,
			cost_tokens, execution_duration_seconds, status,
			model, claude_task_id, source, step_id,
			created_at, updated_at
		) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?)`,
		e.EventID, e.AgentID, string(e.EventType),
		e.Timestamp.UTC().Format(time.RFC3339), nullable(e.ToolName),
		nullable(e.InputSummary), nullable(e.ToolInput), nullable(e.OutputSummary),
		e.SessionID, nullable(e.FeatureID),
		nullable(e.ParentAgentID), nullable(e.ParentEventID), nullable(e.SubagentType),
		e.CostTokens, e.ExecDuration, e.Status,
		nullable(e.Model), nullable(e.ClaudeTaskID), e.Source, nullable(e.StepID),
		e.CreatedAt.UTC().Format(time.RFC3339), e.UpdatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert event %s: %w", e.EventID, err)
	}
	return nil
}

// UpsertSession inserts a sessions row, or upgrades a '__hook__' placeholder.
func UpsertSession(database *sql.DB, s *models.Session) error {
	_, err := database.Exec(`
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
		s.SessionID, s.AgentAssigned, nullable(s.ParentSessionID),
		nullable(s.ParentEventID), s.CreatedAt.UTC().Format(time.RFC3339),
		s.Status, nullable(s.StartCommit), s.IsSubagent, nullable(s.Model),
		nullable(s.ActiveFeatureID), nullable(s.GitRemoteURL), nullable(s.ProjectDir),
		nullable(s.ExecWorktreePath), nullable(s.Branch), nullable(s.Harness),
		nullable(s.ContinuedFrom))
	if err != nil {
		return fmt.Errorf("upsert session %s: %w", s.SessionID, err)
	}
	return nil
}

// InsertGitCommit writes one git_commits row; a duplicate is ignored.
// feature_id binds as a plain string because it is part of the primary key.
func InsertGitCommit(database *sql.DB, c *models.GitCommit) error {
	_, err := database.Exec(`
		INSERT OR IGNORE INTO git_commits (
			commit_hash, session_id, feature_id, tool_event_id, message, timestamp
		) VALUES (?, ?, ?, ?, ?, ?)`,
		c.CommitHash, c.SessionID, c.FeatureID, nullable(c.ToolEventID),
		nullable(c.Message), c.Timestamp.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert git commit %s: %w", c.CommitHash, err)
	}
	return nil
}

// ActiveWorkItem is the work item in active_work_items for (sessionID, agentID).
func ActiveWorkItem(database *sql.DB, sessionID, agentID string) string {
	if database == nil {
		return ""
	}
	var id sql.NullString
	database.QueryRow(
		`SELECT work_item_id FROM active_work_items WHERE session_id = ? AND agent_id = ?`,
		sessionID, agentID).Scan(&id)
	return id.String
}

// ActiveFeatureIDForSession is sessions.active_feature_id for sessionID.
func ActiveFeatureIDForSession(database *sql.DB, sessionID string) string {
	if database == nil {
		return ""
	}
	var id sql.NullString
	database.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ?`, sessionID).Scan(&id)
	return id.String
}

// UpsertPendingSubagentStart inserts or replaces one pending_subagent_starts row.
func UpsertPendingSubagentStart(database *sql.DB, p *dbpkg.PendingSubagentStart) error {
	_, err := database.Exec(`
		INSERT OR REPLACE INTO pending_subagent_starts
			(agent_id, agent_type, session_id, cwd, parent_agent_id, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		p.AgentID, p.AgentType, p.SessionID, nullable(p.CWD), nullable(p.ParentAgentID), p.CreatedAt)
	if err != nil {
		return fmt.Errorf("upsert pending_subagent_starts: %w", err)
	}
	return nil
}
