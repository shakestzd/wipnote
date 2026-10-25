package hooks

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// userTranscriptRecord is the minimal shape of a user JSONL line in the Claude
// Code transcript. Content is kept as raw JSON because it can be either a plain
// string (legacy) or an array of typed blocks (modern).
type userTranscriptRecord struct {
	Type        string          `json:"type"`
	UUID        string          `json:"uuid"`
	ParentUUID  string          `json:"parentUuid"`
	SessionID   string          `json:"sessionId"`
	RequestID   string          `json:"requestId"`
	Timestamp   string          `json:"timestamp"`
	IsSidechain bool            `json:"isSidechain"`
	Message     json.RawMessage `json:"message"`
}

// userMessagePayload holds the decoded message fields once we've handled
// the two content shapes.
type userMessagePayload struct {
	text string
}

// extractUserText decodes the message field and returns the human text content.
// Returns empty string when:
//   - content is a tool_result (not a human prompt)
//   - content array has only image blocks
//   - content is empty or unrecognizable
func extractUserText(rawMessage json.RawMessage) string {
	if len(rawMessage) == 0 {
		return ""
	}

	// Decode into a struct that gives us raw content for flexible parsing.
	var msg struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(rawMessage, &msg); err != nil {
		return ""
	}

	if len(msg.Content) == 0 {
		return ""
	}

	// Try legacy format: content is a plain string.
	var strContent string
	if err := json.Unmarshal(msg.Content, &strContent); err == nil {
		return strings.TrimSpace(strContent)
	}

	// Try modern format: content is an array of typed blocks.
	var blocks []json.RawMessage
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}

	// Inspect first block — if it's a tool_result, skip this record entirely.
	if len(blocks) > 0 {
		var first struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(blocks[0], &first) == nil && first.Type == "tool_result" {
			return "" // tool results are not human prompts
		}
	}

	// Extract the first text block.
	for _, raw := range blocks {
		var block struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &block); err != nil {
			continue
		}
		if block.Type == "text" && block.Text != "" {
			return strings.TrimSpace(block.Text)
		}
	}

	return "" // image-only or no text blocks
}

// userPromptSignalID returns a deterministic signal_id for a user_prompt signal
// keyed on the record's UUID, ensuring idempotency on repeated backfill runs.
func userPromptSignalID(uuid string) string {
	h := sha256.New()
	h.Write([]byte("user_prompt:" + uuid))
	return fmt.Sprintf("%x", h.Sum(nil))[:32]
}

// backfillMissedUserPrompts scans the transcript JSONL file and inserts
// otel_signals rows for user prompts that were missed by the live hook path.
//
// It is idempotent: records already captured (span_id = uuid exists with
// canonical='user_prompt') are skipped. Running backfill multiple times
// produces no duplicates.
//
// The scan resumes at the offset the previous run saved (feat-c30d0792): Stop
// fires on every model response, and re-decoding the whole transcript each
// time made every turn cost the session's length. Only newline-terminated
// lines advance the offset, so a line still being written is read again.
//
// Returns the count of newly inserted rows. Errors are non-fatal by convention —
// callers should log and continue.
func backfillMissedUserPrompts(database *sql.DB, projectDir, sessionID, transcriptPath string) (int, error) {
	if transcriptPath == "" {
		return 0, nil
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // missing file is not an error
		}
		return 0, fmt.Errorf("open transcript: %w", err)
	}
	defer f.Close()

	key := backfillOffsetKey(sessionID, transcriptPath)
	offset := resumeOffset(f, loadBackfillOffset(database, key))
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek transcript: %w", err)
	}

	var featureID sql.NullString // active feature for attribution (best-effort)
	_ = database.QueryRow(`SELECT work_item_id FROM active_work_items WHERE session_id = ? AND agent_id = ?`,
		sessionID, "__root__").Scan(&featureID)

	inserted := 0
	scanner := bufio.NewScanner(f)
	// Increase buffer for very long lines (large prompts in transcript).
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	lines := &lineTracker{}
	scanner.Split(lines.split)

	consumed := offset
	for scanner.Scan() {
		noteBackfillRead(lines.last)
		if backfillPromptLine(database, projectDir, sessionID, featureID, scanner.Bytes()) {
			inserted++
		}
		if lines.terminated {
			consumed += int64(lines.last)
		}
	}
	saveBackfillOffset(database, projectDir, key, offset, consumed)
	if err := scanner.Err(); err != nil {
		return inserted, fmt.Errorf("scan transcript: %w", err)
	}

	return inserted, nil
}

// backfillPromptLine inserts the user prompt carried by one transcript line,
// if it is one and is not already recorded. Reports whether a row was added.
func backfillPromptLine(database *sql.DB, projectDir, sessionID string, featureID sql.NullString, line []byte) bool {
	if len(bytes.TrimSpace(line)) == 0 {
		return false
	}
	var rec userTranscriptRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return false // malformed line — skip
	}
	if rec.Type != "user" || rec.IsSidechain || rec.UUID == "" {
		return false // not a main-thread user record
	}
	text := extractUserText(rec.Message)
	if text == "" {
		return false // tool_result, image-only, or empty
	}

	attrsMap := map[string]any{"text": text, "source": "transcript_backfill"}
	if rec.RequestID != "" {
		attrsMap["request_id"] = rec.RequestID
	}
	attrsJSON, err := json.Marshal(attrsMap)
	if err != nil {
		return false
	}

	// INSERT OR IGNORE keyed on signal_id for idempotency.
	res, dbErr := database.Exec(`
		INSERT OR IGNORE INTO otel_signals (
			signal_id, harness, session_id,
			span_id, parent_span,
			kind, canonical, native, ts_micros,
			attrs_json, feature_id
		) VALUES (?, 'claude', ?, ?, ?, 'log', 'user_prompt', 'user_turn', ?, ?, ?)`,
		userPromptSignalID(rec.UUID), sessionID,
		nullableStr(rec.UUID), nullableStr(rec.ParentUUID),
		recordTsMicros(rec.Timestamp), string(attrsJSON), featureID,
	)
	if dbErr != nil {
		debugLog(projectDir, "[user-prompt-backfill] insert signal: %v", dbErr)
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

// recordTsMicros parses a transcript record timestamp, falling back to now.
func recordTsMicros(ts string) int64 {
	if ts != "" {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && t.UnixMicro() != 0 {
			return t.UnixMicro()
		}
	}
	return time.Now().UnixMicro()
}
