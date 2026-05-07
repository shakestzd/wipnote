package main

import (
	"database/sql"
	"net/http"
	"strings"
)

// featureActivityHandler returns a timeline of all agent_events attributed to
// a feature, plus a summary of files edited and sessions involved.
// Route: /api/features/{id}/activity   (id extracted from URL path)
func featureActivityHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Extract feature ID from URL path: /api/features/{id}/activity
		path := strings.TrimPrefix(r.URL.Path, "/api/features/")
		path = strings.TrimSuffix(path, "/activity")
		featureID := strings.TrimSpace(path)
		if featureID == "" {
			http.Error(w, "feature id required", http.StatusBadRequest)
			return
		}

		// Look up feature title from DB (graceful fallback to empty string).
		var featureTitle string
		database.QueryRow(`SELECT COALESCE(title,'') FROM features WHERE id = ?`, featureID).Scan(&featureTitle)

		// Query events attributed to this feature.
		rows, err := database.Query(`
			SELECT event_id, timestamp, COALESCE(tool_name,''), COALESCE(input_summary,''),
			       COALESCE(status,''), session_id
			FROM agent_events
			WHERE feature_id = ?
			ORDER BY timestamp DESC
			LIMIT 200`, featureID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type eventRow struct {
			EventID      string `json:"event_id"`
			Timestamp    string `json:"timestamp"`
			ToolName     string `json:"tool_name"`
			InputSummary string `json:"input_summary"`
			Status       string `json:"status"`
			SessionID    string `json:"session_id"`
		}

		events := make([]eventRow, 0, 100)
		sessionSet := make(map[string]struct{})
		for rows.Next() {
			var ev eventRow
			if err := rows.Scan(&ev.EventID, &ev.Timestamp, &ev.ToolName,
				&ev.InputSummary, &ev.Status, &ev.SessionID); err != nil {
				continue
			}
			events = append(events, ev)
			if ev.SessionID != "" {
				sessionSet[ev.SessionID] = struct{}{}
			}
		}

		// Query file edits grouped by file path.
		fileRows, err := database.Query(`
			SELECT file_path, COUNT(*) AS edit_count, MAX(last_seen) AS last_edit
			FROM feature_files
			WHERE feature_id = ?
			GROUP BY file_path
			ORDER BY last_edit DESC`, featureID)

		type fileEdit struct {
			FilePath  string `json:"file_path"`
			EditCount int    `json:"edit_count"`
			LastEdit  string `json:"last_edit"`
		}

		fileEdits := make([]fileEdit, 0, 20)
		if err == nil {
			defer fileRows.Close()
			for fileRows.Next() {
				var fe fileEdit
				if err := fileRows.Scan(&fe.FilePath, &fe.EditCount, &fe.LastEdit); err != nil {
					continue
				}
				fileEdits = append(fileEdits, fe)
			}
		}

		// Query git commits linked to this feature.
		commitRows, err := database.Query(`
			SELECT commit_hash, COALESCE(message,''), COALESCE(timestamp,'')
			FROM git_commits
			WHERE feature_id = ?
			ORDER BY timestamp DESC
			LIMIT 50`, featureID)

		type commitRow struct {
			SHA       string `json:"sha"`
			Subject   string `json:"subject"`
			Timestamp string `json:"timestamp"`
		}

		commits := make([]commitRow, 0, 10)
		if err == nil {
			defer commitRows.Close()
			for commitRows.Next() {
				var cr commitRow
				var fullMsg string
				if err := commitRows.Scan(&cr.SHA, &fullMsg, &cr.Timestamp); err != nil {
					continue
				}
				// Use first line of commit message as subject.
				if nl := strings.IndexByte(fullMsg, '\n'); nl >= 0 {
					cr.Subject = fullMsg[:nl]
				} else {
					cr.Subject = fullMsg
				}
				commits = append(commits, cr)
			}
		}

		// Build unique session list preserving discovery order.
		sessions := make([]string, 0, len(sessionSet))
		seen := make(map[string]bool)
		for _, ev := range events {
			if ev.SessionID != "" && !seen[ev.SessionID] {
				sessions = append(sessions, ev.SessionID)
				seen[ev.SessionID] = true
			}
		}

		respondJSON(w, map[string]any{
			"feature_id":    featureID,
			"feature_title": featureTitle,
			"total_events":  len(events),
			"events":        events,
			"file_edits":    fileEdits,
			"commits":       commits,
			"sessions":      sessions,
		})
	}
}

// featureActivityRouter dispatches /api/features/ sub-routes.
// It handles both /api/features/detail and /api/features/{id}/activity,
// delegating unknown paths to a 404.
func featureActivityRouter(database *sql.DB, wipnoteDir string) http.HandlerFunc {
	detailHandler := featureDetailHandler(wipnoteDir)
	relatedHandler := relatedFeaturesHandler(database)
	activityHandler := featureActivityHandler(database)

	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/api/features/detail":
			detailHandler(w, r)
		case path == "/api/features/related":
			relatedHandler(w, r)
		case strings.HasSuffix(path, "/activity"):
			activityHandler(w, r)
		default:
			http.NotFound(w, r)
		}
	}
}
