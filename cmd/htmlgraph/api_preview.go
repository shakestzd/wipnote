package main

import (
	"database/sql"
	"net/http"
	"strconv"
)

const (
	previewDefaultN    = 2
	previewMaxN        = 20
	previewTruncateLen = 200
)

// previewMessage is a single item in the preview response.
type previewMessage struct {
	Role             string `json:"role"`
	ContentTruncated string `json:"content_truncated"`
}

// previewResponse is the JSON body returned by GET /api/sessions/{id}/preview.
type previewResponse struct {
	SessionID string           `json:"session_id"`
	Messages  []previewMessage `json:"messages"`
}

// previewHandler handles GET /api/sessions/{id}/preview.
// It returns the last N user/assistant messages (default N=2) with role and
// truncated content (200 runes). Sessions with no messages return 200 + empty
// array, not 404.
func previewHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessionID, suffix := extractSessionIDWithSuffix(r.URL.Path)
		if sessionID == "" || suffix != "/preview" {
			http.Error(w, "missing session ID", http.StatusBadRequest)
			return
		}

		n := previewDefaultN
		if nStr := r.URL.Query().Get("n"); nStr != "" {
			if parsed, err := strconv.Atoi(nStr); err == nil && parsed > 0 && parsed <= previewMaxN {
				n = parsed
			}
		}

		msgs, err := previewMessages(database, sessionID, n)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		respondJSON(w, previewResponse{
			SessionID: sessionID,
			Messages:  msgs,
		})
	}
}

// previewMessages queries the last N user/assistant messages for the given
// session, returning them in chronological order (oldest of the N first).
// The content of each message is truncated to previewTruncateLen runes.
func previewMessages(database *sql.DB, sessionID string, n int) ([]previewMessage, error) {
	// Fetch last N rows ordered DESC, then reverse to get chronological order.
	// Exclude rows whose content is NULL or trims to empty — assistant turns
	// that were purely tool-calls have no user-facing content and would
	// surface as blank preview rows if included.
	rows, err := database.Query(`
		SELECT role, content
		FROM messages
		WHERE session_id = ?
		  AND role IN ('user', 'assistant')
		  AND content IS NOT NULL
		  AND TRIM(content) <> ''
		ORDER BY ordinal DESC
		LIMIT ?`, sessionID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect in DESC order then reverse.
	var reversed []previewMessage
	for rows.Next() {
		var role, content string
		if err := rows.Scan(&role, &content); err != nil {
			continue
		}
		reversed = append(reversed, previewMessage{
			Role:             role,
			ContentTruncated: truncateRunes(content, previewTruncateLen),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Reverse to chronological order.
	msgs := make([]previewMessage, len(reversed))
	for i, m := range reversed {
		msgs[len(reversed)-1-i] = m
	}
	return msgs, nil
}

// truncateRunes truncates s to at most maxRunes runes (unicode-safe).
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}
