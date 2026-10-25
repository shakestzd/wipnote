package hooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/db"
)

type sessionEndSnapshot struct {
	ActiveFeatureID  string
	ParentSessionID  string
	ProjectDir       string
	ExecWorktreePath string
	Branch           string
	Harness          string
	TranscriptPath   string
	LastUserQuery    string
	ExistingHandoff  string
	ExistingNext     string
	ExistingBlockers string
	ExistingContext  string
}

type sessionHandoffContext struct {
	WorkItemID       string   `json:"work_item_id,omitempty"`
	LastSessionID    string   `json:"last_session_id,omitempty"`
	ParentSessionID  string   `json:"parent_session_id,omitempty"`
	ProjectDir       string   `json:"project_dir,omitempty"`
	ExecWorktreePath string   `json:"exec_worktree_path,omitempty"`
	Branch           string   `json:"branch,omitempty"`
	Harness          string   `json:"harness,omitempty"`
	TranscriptPath   string   `json:"transcript_path,omitempty"`
	EndReason        string   `json:"end_reason,omitempty"`
	LastUserQuery    string   `json:"last_user_query,omitempty"`
	FeaturesWorkedOn []string `json:"features_worked_on,omitempty"`
	Files            []string `json:"files,omitempty"`
	RecentActivity   []string `json:"recent_activity,omitempty"`
	SummarySources   []string `json:"summary_sources,omitempty"`
}

func persistSessionEndHandoff(database *sql.DB, sessionID string, event *CloudEvent, projectDir string, featuresWorkedOn []string) error {
	snapshot, err := loadSessionEndSnapshot(database, sessionID)
	if err != nil {
		return err
	}
	if snapshot.ActiveFeatureID == "" && len(featuresWorkedOn) > 0 {
		snapshot.ActiveFeatureID = featuresWorkedOn[0]
	}

	activities, blockers, err := recentSessionActivity(database, sessionID)
	if err != nil {
		return err
	}
	if snapshot.ActiveFeatureID == "" && snapshot.LastUserQuery == "" && len(activities) == 0 && len(blockers) == 0 {
		return nil
	}

	// File pointers come from the canonical tool-events.ndjson log, not
	// feature_files: posttooluse was that table's only writer and its rows
	// landed on a handle no session-end reader shares (bug-07fd4a51).
	filePointers := recentCanonicalFilePointers(projectDir, sessionID, func(p string) string {
		return normalizeSessionPointer(projectDir, snapshot.ProjectDir, p)
	})

	handoffNotes := buildHandoffNotes(snapshot.ActiveFeatureID, snapshot.LastUserQuery, activities, blockers)
	recommendedNext := buildRecommendedNext(snapshot.ActiveFeatureID, snapshot.ExecWorktreePath, snapshot.Branch, snapshot.Harness, snapshot.LastUserQuery)
	context := newSessionHandoffContext(snapshot, sessionID, event.Reason, featuresWorkedOn, filePointers, activities)

	var blockersJSON []byte
	if len(blockers) > 0 {
		blockersJSON, err = json.Marshal(blockers)
		if err != nil {
			return fmt.Errorf("marshal blockers: %w", err)
		}
	}

	contextJSON, err := json.Marshal(context)
	if err != nil {
		return fmt.Errorf("marshal recommended_context: %w", err)
	}
	if handoffNotes == "" && recommendedNext == "" && len(blockersJSON) == 0 && string(contextJSON) == "{}" {
		return nil
	}

	return db.UpdateSessionHandoff(database, sessionID, handoffNotes, recommendedNext, blockersJSON, contextJSON)
}

// newSessionHandoffContext assembles the recommended_context recorded on the
// session at SessionEnd.
func newSessionHandoffContext(snapshot *sessionEndSnapshot, sessionID, endReason string, featuresWorkedOn, filePointers, activities []string) sessionHandoffContext {
	sources := make([]string, 0, 2)
	if snapshot.LastUserQuery != "" {
		sources = append(sources, "last_user_query")
	}
	if len(activities) > 0 {
		sources = append(sources, "agent_events")
	}

	return sessionHandoffContext{
		WorkItemID:       snapshot.ActiveFeatureID,
		LastSessionID:    sessionID,
		ParentSessionID:  snapshot.ParentSessionID,
		ProjectDir:       snapshot.ProjectDir,
		ExecWorktreePath: snapshot.ExecWorktreePath,
		Branch:           snapshot.Branch,
		Harness:          snapshot.Harness,
		TranscriptPath:   snapshot.TranscriptPath,
		EndReason:        endReason,
		LastUserQuery:    snapshot.LastUserQuery,
		FeaturesWorkedOn: featuresWorkedOn,
		Files:            filePointers,
		RecentActivity:   activities,
		SummarySources:   sources,
	}
}

func loadSessionEndSnapshot(database *sql.DB, sessionID string) (*sessionEndSnapshot, error) {
	row := database.QueryRow(`
		SELECT
			COALESCE(active_feature_id, ''),
			COALESCE(parent_session_id, ''),
			COALESCE(project_dir, ''),
			COALESCE(exec_worktree_path, ''),
			COALESCE(branch, ''),
			COALESCE(harness, ''),
			COALESCE(transcript_path, ''),
			COALESCE(last_user_query, ''),
			COALESCE(handoff_notes, ''),
			COALESCE(recommended_next, ''),
			COALESCE(blockers, ''),
			COALESCE(recommended_context, '')
		FROM sessions
		WHERE session_id = ?`,
		sessionID,
	)

	var snapshot sessionEndSnapshot
	if err := row.Scan(
		&snapshot.ActiveFeatureID,
		&snapshot.ParentSessionID,
		&snapshot.ProjectDir,
		&snapshot.ExecWorktreePath,
		&snapshot.Branch,
		&snapshot.Harness,
		&snapshot.TranscriptPath,
		&snapshot.LastUserQuery,
		&snapshot.ExistingHandoff,
		&snapshot.ExistingNext,
		&snapshot.ExistingBlockers,
		&snapshot.ExistingContext,
	); err != nil {
		return nil, fmt.Errorf("load session handoff snapshot %s: %w", sessionID, err)
	}
	return &snapshot, nil
}

func recentSessionActivity(database *sql.DB, sessionID string) ([]string, []string, error) {
	rows, err := database.Query(`
		SELECT COALESCE(tool_name, ''),
		       COALESCE(input_summary, ''),
		       COALESCE(output_summary, ''),
		       COALESCE(status, '')
		FROM agent_events
		WHERE session_id = ?
		  AND COALESCE(tool_name, '') NOT IN ('', 'UserQuery')
		ORDER BY timestamp DESC
		LIMIT 6`,
		sessionID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("recent session activity %s: %w", sessionID, err)
	}
	defer rows.Close()

	activity := make([]string, 0, 4)
	blockers := make([]string, 0, 2)
	for rows.Next() {
		var toolName, inputSummary, outputSummary, status string
		if err := rows.Scan(&toolName, &inputSummary, &outputSummary, &status); err != nil {
			return nil, nil, fmt.Errorf("scan recent session activity %s: %w", sessionID, err)
		}
		summary := strings.TrimSpace(firstNonEmpty(inputSummary, outputSummary, toolName))
		if summary != "" {
			activity = append(activity, trimForHandoff(summary, 180))
		}
		if status == "failed" && outputSummary != "" {
			blockers = append(blockers, trimForHandoff(outputSummary, 300))
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate recent session activity %s: %w", sessionID, err)
	}
	return activity, blockers, nil
}

func buildHandoffNotes(workItemID, lastUserQuery string, activities, blockers []string) string {
	parts := make([]string, 0, 4)
	if workItemID != "" {
		parts = append(parts, "Work item: "+workItemID+".")
	}
	if lastUserQuery != "" {
		parts = append(parts, "Last user request: "+trimForHandoff(lastUserQuery, 220))
	}
	if len(activities) > 0 {
		parts = append(parts, "Recent activity:\n- "+strings.Join(activities, "\n- "))
	}
	if len(blockers) > 0 {
		parts = append(parts, "Recent failure traces:\n- "+strings.Join(blockers, "\n- "))
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func buildRecommendedNext(workItemID, execWorktreePath, branch, harness, lastUserQuery string) string {
	target := "Resume the last task"
	if workItemID != "" {
		target = "Resume " + workItemID
	}
	context := make([]string, 0, 3)
	if execWorktreePath != "" {
		context = append(context, execWorktreePath)
	}
	if branch != "" {
		context = append(context, "branch "+branch)
	}
	if harness != "" {
		context = append(context, "via "+harness)
	}
	if len(context) > 0 {
		target += " in " + strings.Join(context, " on ")
	}
	if lastUserQuery != "" {
		target += ". Start from: " + trimForHandoff(lastUserQuery, 180)
	}
	return target
}

func normalizeSessionPointer(projectDir, canonicalProjectDir, path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	for _, base := range []string{projectDir, canonicalProjectDir} {
		if base == "" || !filepath.IsAbs(base) {
			continue
		}
		if rel, err := filepath.Rel(base, path); err == nil && rel != "" && rel != "." && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}

func trimForHandoff(s string, limit int) string {
	s = strings.TrimSpace(s)
	if limit <= 0 || len([]rune(s)) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
