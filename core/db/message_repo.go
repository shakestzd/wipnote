package db

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// InsertMessage stores a transcript message. Returns the auto-generated ID.
func InsertMessage(db *sql.DB, m *models.Message) (int64, error) {
	res, err := db.Exec(`
		INSERT OR IGNORE INTO messages
			(session_id, agent_id, ordinal, role, content, timestamp,
			 has_thinking, has_tool_use, content_length,
			 model, input_tokens, output_tokens, cache_read_tokens,
			 stop_reason, uuid, parent_uuid)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.SessionID, nullStr(m.AgentID), m.Ordinal, m.Role, m.Content,
		m.Timestamp.UTC().Format(time.RFC3339Nano),
		boolToInt(m.HasThinking), boolToInt(m.HasToolUse), m.ContentLength,
		nullStr(m.Model), m.InputTokens, m.OutputTokens, m.CacheReadTokens,
		nullStr(m.StopReason), nullStr(m.UUID), nullStr(m.ParentUUID),
	)
	if err != nil {
		return 0, fmt.Errorf("insert message (session %s ord %d): %w", m.SessionID, m.Ordinal, err)
	}
	return res.LastInsertId()
}

// InsertToolCall stores a tool call extracted from a message.
func InsertToolCall(db *sql.DB, tc *models.ToolCall) error {
	_, err := db.Exec(`
		INSERT INTO tool_calls
			(message_id, session_id, tool_name, category, tool_use_id,
			 input_json, result_content_length, subagent_session_id, feature_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tc.MessageID, tc.SessionID, tc.ToolName, tc.Category,
		nullStr(tc.ToolUseID), nullStr(tc.InputJSON),
		tc.ResultContentLength, nullStr(tc.SubagentSessionID), nullStr(tc.FeatureID),
	)
	if err != nil {
		return fmt.Errorf("insert tool_call: %w", err)
	}
	return nil
}

// UpdateTranscriptSync marks a session as transcript-synced.
func UpdateTranscriptSync(db *sql.DB, sessionID, transcriptPath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.Exec(`
		UPDATE sessions
		SET transcript_path = ?, transcript_synced = ?
		WHERE session_id = ?`,
		transcriptPath, now, sessionID,
	)
	return err
}

// DeleteSessionMessages removes all messages and tool_calls for a session.
func DeleteSessionMessages(db *sql.DB, sessionID string) error {
	_, err := db.Exec(`DELETE FROM messages WHERE session_id = ?`, sessionID)
	if err != nil {
		return fmt.Errorf("delete messages for %s: %w", sessionID, err)
	}
	_, err = db.Exec(`DELETE FROM tool_calls WHERE session_id = ?`, sessionID)
	return err
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
