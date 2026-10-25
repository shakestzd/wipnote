package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

type ResumableSession struct {
	WorkItemID       string `json:"work_item_id"`
	Title            string `json:"title"`
	Type             string `json:"type"`
	PromptLabel      string `json:"prompt_label"`
	Branch           string `json:"branch"`
	ExecWorktreePath string `json:"exec_worktree_path"`
	Harness          string `json:"harness"`
	LastActivity     string `json:"last_activity"`
	LastSessionID    string `json:"last_session_id"`
	Live             bool   `json:"live"`
	// Status is the work item's status (todo, in-progress, blocked, ...); the
	// launcher picker offers only in-progress items. Empty for rows without one.
	Status string `json:"status,omitempty"`
}

type HarnessGroupedResumableSessions struct {
	// Current, when non-nil, is the session the user is launching from (resolved
	// from the harness/launcher env + its session family). It is surfaced as a
	// first-class "Resume this session" slot at the top of the chooser and
	// deliberately bypasses the work_item_id and item_status gates the grouped
	// listings apply, so a current session with no (or completed) work item is
	// still offered. Nil when no current session can be resolved.
	Current *ResumableSession `json:"current,omitempty"`
	// CurrentIsLast marks Current as the project's most recent session (read from
	// the harness's own transcripts) rather than the session the launch runs
	// inside, so the picker can label it "Continue last session".
	CurrentIsLast bool               `json:"current_is_last,omitempty"`
	SameHarness   []ResumableSession `json:"same_harness"`
	CrossHarness  []ResumableSession `json:"cross_harness"`
}

// ListResumableSessions returns the most-recent session per incomplete work
// item, ranked by last activity descending.
func ListResumableSessions(db *sql.DB, threshold time.Duration) ([]ResumableSession, error) {
	if db == nil {
		return nil, nil
	}
	cutoff := time.Now().UTC().Add(-threshold).Format(time.RFC3339)
	rows, err := db.Query(`
WITH session_work AS (
	-- One row per (session, work_item): JOIN against active_work_items so
	-- parallel agents in the same session each get their own row. Fall back
	-- to active_feature_id when no active_work_items row exists (legacy sessions).
	SELECT
		s.session_id,
		s.created_at,
		COALESCE(s.exec_worktree_path, '') AS exec_worktree_path,
		COALESCE(s.branch, '') AS branch,
		COALESCE(s.harness, '') AS harness,
		COALESCE(s.agent_assigned, '') AS agent_assigned,
		COALESCE(awi.work_item_id, s.active_feature_id, '') AS work_item_id
	FROM sessions s
	LEFT JOIN active_work_items awi ON awi.session_id = s.session_id
),
enriched AS (
	SELECT
		sw.session_id,
		sw.work_item_id,
		COALESCE(f.title, t.title, '') AS title,
		COALESCE(NULLIF(f.type, ''), CASE WHEN t.id IS NOT NULL THEN 'track' ELSE '' END) AS type,
		COALESCE(f.status, t.status, '') AS item_status,
		sw.branch,
		sw.exec_worktree_path,
		sw.harness,
		sw.agent_assigned,
		COALESCE((
			SELECT MAX(ts)
			FROM (
				SELECT MAX(ae.timestamp) AS ts FROM agent_events ae WHERE ae.session_id = sw.session_id
				UNION ALL
				SELECT MAX(m.timestamp) AS ts FROM messages m WHERE m.session_id = sw.session_id
				UNION ALL
				SELECT MAX(c.last_heartbeat_at) AS ts FROM claims c WHERE c.owner_session_id = sw.session_id
				UNION ALL
				SELECT sw.created_at AS ts
			)
		), sw.created_at) AS last_activity,
		(SELECT MAX(c.last_heartbeat_at) FROM claims c WHERE c.owner_session_id = sw.session_id) AS last_heartbeat_at
	FROM session_work sw
	LEFT JOIN features f ON f.id = sw.work_item_id
	LEFT JOIN tracks t ON t.id = sw.work_item_id
	WHERE sw.work_item_id <> ''
),
ranked AS (
	SELECT
		*,
		ROW_NUMBER() OVER (
			PARTITION BY work_item_id
			ORDER BY last_activity DESC, session_id DESC
		) AS row_num
	FROM enriched
	WHERE item_status NOT IN ('done', 'completed')
)
SELECT
	work_item_id,
	title,
	type,
	branch,
	exec_worktree_path,
	harness,
	last_activity,
	session_id,
	CASE
		WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at >= ? THEN 1
		ELSE 0
	END AS live,
	agent_assigned
FROM ranked
WHERE row_num = 1
ORDER BY last_activity DESC, work_item_id`, cutoff)
	if err != nil {
		return nil, fmt.Errorf("list resumable sessions: %w", err)
	}
	defer rows.Close()

	var out []ResumableSession
	for rows.Next() {
		var item ResumableSession
		var live int
		var agentAssigned string
		if err := rows.Scan(
			&item.WorkItemID,
			&item.Title,
			&item.Type,
			&item.Branch,
			&item.ExecWorktreePath,
			&item.Harness,
			&item.LastActivity,
			&item.LastSessionID,
			&live,
			&agentAssigned,
		); err != nil {
			return nil, fmt.Errorf("scan resumable session: %w", err)
		}
		item.Live = live != 0
		item.Harness = normalizeSessionHarness(item.Harness, agentAssigned)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Enrich AFTER the cursor is closed. SessionPromptLabel runs its own query,
	// and the ephemeral projection allows a single connection, so calling it
	// inside the loop deadlocks — see the note in
	// GetLatestHarnessSessionResumable (bug-8e9ceb7b).
	rows.Close()
	for i := range out {
		out[i].PromptLabel = SessionPromptLabel(db, out[i].LastSessionID)
	}
	return out, nil
}

// ListHarnessGroupedResumableSessions returns resumable sessions split into
// current-harness and cross-harness buckets. Same-harness rows keep the most
// recent session for each work item on that harness, even when a newer session
// from another harness exists for the same item.
func ListHarnessGroupedResumableSessions(db *sql.DB, threshold time.Duration, harness string) (HarnessGroupedResumableSessions, error) {
	var grouped HarnessGroupedResumableSessions
	if db == nil {
		return grouped, nil
	}
	cutoff := time.Now().UTC().Add(-threshold).Format(time.RFC3339)
	targetHarness := strings.TrimSpace(strings.ToLower(harness))
	rows, err := db.Query(harnessGroupedResumableSessionsSQL, targetHarness, targetHarness, cutoff)
	if err != nil {
		return grouped, fmt.Errorf("list grouped resumable sessions: %w", err)
	}
	defer rows.Close()

	grouped, err = scanHarnessGroupedResumableSessions(rows)
	if err != nil {
		return grouped, err
	}
	// Enrich AFTER the cursor is closed — SessionPromptLabel issues its own
	// query and the projection allows a single connection (bug-8e9ceb7b).
	rows.Close()
	for i := range grouped.SameHarness {
		grouped.SameHarness[i].PromptLabel = SessionPromptLabel(db, grouped.SameHarness[i].LastSessionID)
	}
	for i := range grouped.CrossHarness {
		grouped.CrossHarness[i].PromptLabel = SessionPromptLabel(db, grouped.CrossHarness[i].LastSessionID)
	}
	return grouped, nil
}

// scanHarnessGroupedResumableSessions scans the grouped resumable-session
// rows into the same-harness and cross-harness buckets.
func scanHarnessGroupedResumableSessions(rows *sql.Rows) (HarnessGroupedResumableSessions, error) {
	var grouped HarnessGroupedResumableSessions
	for rows.Next() {
		var item ResumableSession
		var live int
		var agentAssigned string
		var harnessGroup int
		if err := rows.Scan(
			&item.WorkItemID,
			&item.Title,
			&item.Type,
			&item.Branch,
			&item.ExecWorktreePath,
			&item.Harness,
			&item.LastActivity,
			&item.LastSessionID,
			&live,
			&agentAssigned,
			&harnessGroup,
			&item.Status,
		); err != nil {
			return grouped, fmt.Errorf("scan grouped resumable session: %w", err)
		}
		item.Live = live != 0
		item.Harness = normalizeSessionHarness(item.Harness, agentAssigned)
		if harnessGroup == 0 {
			grouped.SameHarness = append(grouped.SameHarness, item)
		} else {
			grouped.CrossHarness = append(grouped.CrossHarness, item)
		}
	}
	if err := rows.Err(); err != nil {
		return grouped, err
	}
	return grouped, nil
}

// harnessGroupedResumableSessionsSQL selects the most recent session per work
// item, ranked into same-harness (group 0) and cross-harness (group 1)
// buckets. Parameters: target harness (twice), live-heartbeat cutoff.
const harnessGroupedResumableSessionsSQL = `
WITH session_work AS (
	SELECT
		s.session_id,
		s.created_at,
		COALESCE(s.exec_worktree_path, '') AS exec_worktree_path,
		COALESCE(s.branch, '') AS branch,
		COALESCE(s.harness, '') AS harness,
		COALESCE(s.agent_assigned, '') AS agent_assigned,
		COALESCE(awi.work_item_id, s.active_feature_id, '') AS work_item_id
	FROM sessions s
	LEFT JOIN active_work_items awi ON awi.session_id = s.session_id
),
enriched AS (
	SELECT
		sw.session_id,
		sw.work_item_id,
		COALESCE(f.title, t.title, '') AS title,
		COALESCE(NULLIF(f.type, ''), CASE WHEN t.id IS NOT NULL THEN 'track' ELSE '' END) AS type,
		COALESCE(f.status, t.status, '') AS item_status,
		sw.branch,
		sw.exec_worktree_path,
		sw.harness,
		sw.agent_assigned,
		COALESCE((
			SELECT MAX(ts)
			FROM (
				SELECT MAX(ae.timestamp) AS ts FROM agent_events ae WHERE ae.session_id = sw.session_id
				UNION ALL
				SELECT MAX(m.timestamp) AS ts FROM messages m WHERE m.session_id = sw.session_id
				UNION ALL
				SELECT MAX(c.last_heartbeat_at) AS ts FROM claims c WHERE c.owner_session_id = sw.session_id
				UNION ALL
				SELECT sw.created_at AS ts
			)
		), sw.created_at) AS last_activity,
		(SELECT MAX(c.last_heartbeat_at) FROM claims c WHERE c.owner_session_id = sw.session_id) AS last_heartbeat_at
	FROM session_work sw
	LEFT JOIN features f ON f.id = sw.work_item_id
	LEFT JOIN tracks t ON t.id = sw.work_item_id
	WHERE sw.work_item_id <> ''
),
normalized AS (
	SELECT
		session_id,
		work_item_id,
		title,
		type,
		item_status,
		branch,
		exec_worktree_path,
		CASE
			-- Older ledger rows record Claude as "claude-code"; they are Claude
			-- sessions and must group (and resume) with the Claude harness.
			WHEN LOWER(TRIM(harness)) IN ('claude-code', 'claude_code') THEN 'claude'
			WHEN TRIM(harness) <> '' THEN LOWER(TRIM(harness))
			WHEN LOWER(TRIM(agent_assigned)) LIKE '%antigravity%' THEN 'antigravity'
			WHEN LOWER(TRIM(agent_assigned)) LIKE '%gemini%' THEN 'gemini'
			WHEN LOWER(TRIM(agent_assigned)) LIKE '%codex%' THEN 'codex'
			WHEN LOWER(TRIM(agent_assigned)) LIKE '%claude%' THEN 'claude'
			ELSE ''
		END AS normalized_harness,
		agent_assigned,
		last_activity,
		last_heartbeat_at
	FROM enriched
	WHERE item_status NOT IN ('done', 'completed')
),
ranked AS (
	SELECT
		*,
		CASE
			WHEN normalized_harness = ? THEN 0
			ELSE 1
		END AS harness_group,
		ROW_NUMBER() OVER (
			PARTITION BY work_item_id,
				CASE WHEN normalized_harness = ? THEN 0 ELSE 1 END
			ORDER BY last_activity DESC, session_id DESC
		) AS row_num
	FROM normalized
)
SELECT
	work_item_id,
	title,
	type,
	branch,
	exec_worktree_path,
	normalized_harness,
	last_activity,
	session_id,
	CASE
		WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at >= ? THEN 1
		ELSE 0
	END AS live,
	agent_assigned,
	harness_group,
	item_status
FROM ranked
WHERE row_num = 1
ORDER BY harness_group ASC, last_activity DESC, work_item_id`

// GetResumableSessionForSessionAndWorkItem resolves one resumable-session row
// for the exact (session_id, work_item_id) pair using the same active_work_items
// + legacy active_feature_id model as the resumable listings.
func GetResumableSessionForSessionAndWorkItem(db *sql.DB, threshold time.Duration, sessionID, workItemID string) (*ResumableSession, error) {
	if db == nil {
		return nil, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	workItemID = strings.TrimSpace(workItemID)
	if sessionID == "" || workItemID == "" {
		return nil, nil
	}
	cutoff := time.Now().UTC().Add(-threshold).Format(time.RFC3339)
	row := db.QueryRow(`
WITH session_work AS (
	SELECT
		s.session_id,
		s.created_at,
		COALESCE(s.exec_worktree_path, '') AS exec_worktree_path,
		COALESCE(s.branch, '') AS branch,
		COALESCE(s.harness, '') AS harness,
		COALESCE(s.agent_assigned, '') AS agent_assigned,
		COALESCE(awi.work_item_id, s.active_feature_id, '') AS work_item_id
	FROM sessions s
	LEFT JOIN active_work_items awi ON awi.session_id = s.session_id
	WHERE s.session_id = ?
),
enriched AS (
	SELECT
		sw.session_id,
		sw.work_item_id,
		COALESCE(f.title, t.title, '') AS title,
		COALESCE(NULLIF(f.type, ''), CASE WHEN t.id IS NOT NULL THEN 'track' ELSE '' END) AS type,
		COALESCE(f.status, t.status, '') AS item_status,
		sw.branch,
		sw.exec_worktree_path,
		sw.harness,
		sw.agent_assigned,
		COALESCE((
			SELECT MAX(ts)
			FROM (
				SELECT MAX(ae.timestamp) AS ts FROM agent_events ae WHERE ae.session_id = sw.session_id
				UNION ALL
				SELECT MAX(m.timestamp) AS ts FROM messages m WHERE m.session_id = sw.session_id
				UNION ALL
				SELECT MAX(c.last_heartbeat_at) AS ts FROM claims c WHERE c.owner_session_id = sw.session_id
				UNION ALL
				SELECT sw.created_at AS ts
			)
		), sw.created_at) AS last_activity,
		(SELECT MAX(c.last_heartbeat_at) FROM claims c WHERE c.owner_session_id = sw.session_id) AS last_heartbeat_at
	FROM session_work sw
	LEFT JOIN features f ON f.id = sw.work_item_id
	LEFT JOIN tracks t ON t.id = sw.work_item_id
	WHERE sw.work_item_id = ?
)
SELECT
	work_item_id,
	title,
	type,
	branch,
	exec_worktree_path,
	harness,
	last_activity,
	session_id,
	CASE
		WHEN last_heartbeat_at IS NOT NULL AND last_heartbeat_at >= ? THEN 1
		ELSE 0
	END AS live,
	agent_assigned
FROM enriched
WHERE item_status NOT IN ('done', 'completed')
ORDER BY last_activity DESC, session_id DESC
LIMIT 1`, sessionID, workItemID, cutoff)

	var item ResumableSession
	var live int
	var agentAssigned string
	if err := row.Scan(
		&item.WorkItemID,
		&item.Title,
		&item.Type,
		&item.Branch,
		&item.ExecWorktreePath,
		&item.Harness,
		&item.LastActivity,
		&item.LastSessionID,
		&live,
		&agentAssigned,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get resumable session for %s/%s: %w", sessionID, workItemID, err)
	}
	item.Live = live != 0
	item.Harness = normalizeSessionHarness(item.Harness, agentAssigned)
	item.PromptLabel = SessionPromptLabel(db, item.LastSessionID)
	return &item, nil
}

func normalizeSessionHarness(raw, agentAssigned string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "claude-code", "claude_code":
		return "claude"
	case "":
	default:
		return raw
	}
	v := strings.ToLower(strings.TrimSpace(agentAssigned))
	switch {
	case strings.Contains(v, "antigravity"):
		return "antigravity"
	case strings.Contains(v, "gemini"):
		return "gemini"
	case strings.Contains(v, "codex"):
		return "codex"
	case strings.Contains(v, "claude"):
		return "claude"
	default:
		return ""
	}
}
