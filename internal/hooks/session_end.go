package hooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/erinn/internal/db"
	"github.com/shakestzd/erinn/internal/otel/materialize"
	"github.com/shakestzd/erinn/internal/paths"
)

// SessionEnd handles the SessionEnd Claude Code hook event.
// It marks the session as completed and records the end commit.
func SessionEnd(event *CloudEvent, database *sql.DB, projectDir string) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	endCommit := headCommit(projectDir)
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := database.Exec(`
		UPDATE sessions
		SET status = 'completed',
		    completed_at = ?,
		    end_commit = COALESCE(NULLIF(?, ''), end_commit)
		WHERE session_id = ?`,
		now, endCommit, sessionID,
	)
	if err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: update sessions: %v", sessionID[:minLen(sessionID, 8)], err)
	}

	// Finalize session HTML file (non-critical, errors silently logged).
	var evtCount int
	_ = database.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id = ?`, sessionID).Scan(&evtCount)
	FinalizeSessionHTML(projectDir, sessionID, now, "completed", evtCount)

	// Store transcript_path and termination reason if provided.
	if event.TranscriptPath != "" || event.Reason != "" {
		_, _ = database.Exec(`
			UPDATE sessions
			SET transcript_path = COALESCE(NULLIF(?, ''), transcript_path),
			    metadata = json_set(COALESCE(metadata, '{}'), '$.end_reason', ?)
			WHERE session_id = ?`,
			event.TranscriptPath, event.Reason, sessionID,
		)
	}

	// Populate features_worked_on from distinct feature_ids in agent_events.
	if feats, fErr := db.DistinctFeatureIDs(database, sessionID); fErr == nil && len(feats) > 0 {
		if featsJSON, jErr := json.Marshal(feats); jErr == nil {
			database.Exec(`UPDATE sessions SET features_worked_on = ? WHERE session_id = ?`,
				string(featsJSON), sessionID)
		}
	}

	// Mark lineage trace complete so tree queries show accurate status.
	if err := db.CompleteLineageTrace(database, sessionID); err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: complete lineage trace: %v", sessionID[:minLen(sessionID, 8)], err)
	}

	// Release all active claims held by this session.
	if released, err := db.ReleaseAllClaimsForSession(database, sessionID); err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: release claims: %v", sessionID[:minLen(sessionID, 8)], err)
	} else if released > 0 {
		debugLog(projectDir, "[htmlgraph] session-end: released %d claims for session %s", released, sessionID[:minLen(sessionID, 8)])
	}

	// Clean up the session-scoped project dir hint file now that this session is ending.
	paths.CleanupSessionHint(sessionID)

	// Backfill any user prompts missed by the live UserPromptSubmit hook path.
	// transcript_path may come from the current event or from the sessions table
	// (written by SessionStart or Stop). Non-fatal: errors are logged only.
	backfillTranscriptPath := event.TranscriptPath
	if backfillTranscriptPath == "" {
		var storedPath sql.NullString
		_ = database.QueryRow(`SELECT transcript_path FROM sessions WHERE session_id = ?`, sessionID).Scan(&storedPath)
		if storedPath.Valid {
			backfillTranscriptPath = storedPath.String
		}
	}
	if backfillTranscriptPath != "" {
		if n, err := backfillMissedUserPrompts(database, projectDir, sessionID, backfillTranscriptPath); err != nil {
			debugLog(projectDir, "[user-prompt-backfill] session-end: %v", err)
		} else if n > 0 {
			debugLog(projectDir, "[user-prompt-backfill] session-end: %d prompts recovered (session=%s)", n, sessionID[:minLen(sessionID, 8)])
		}
	}

	// Signal the per-session OTel collector to drain and exit (Q3 primary layer)
	// BEFORE materializing — the indexer needs the final signals in SQLite first.
	signalCollector(projectDir, sessionID)

	// Wait briefly for the indexer to catch up with the final NDJSON writes.
	waitForIndexerCatchUp(projectDir, sessionID)

	// Materialize OTel rollup (no-op if no signals received for this session).
	// Non-fatal: errors are logged but do not block SessionEnd completion.
	if err := materialize.Materialize(database, projectDir, sessionID); err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: materialize otel: %v", sessionID[:minLen(sessionID, 8)], err)
	}

	return &HookResult{Continue: true}, nil
}

// waitForIndexerCatchUp polls until .index-offset reaches events.ndjson size,
// or 2s elapses. Best-effort — if the indexer is behind, materialize will
// use whatever signals have been indexed so far.
func waitForIndexerCatchUp(projectDir, sessionID string) {
	sessDir := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID)
	ndjsonPath := filepath.Join(sessDir, "events.ndjson")
	offsetPath := filepath.Join(sessDir, ".index-offset")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(ndjsonPath)
		if err != nil {
			return
		}
		data, err := os.ReadFile(offsetPath)
		if err == nil {
			if off, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64); err == nil && off >= info.Size() {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// signalCollector reads the .collector-pid file for this session, sends SIGTERM,
// waits up to 3 seconds for a clean drain, then falls back to SIGKILL.
// All errors are silently logged — the collector PID file is best-effort.
func signalCollector(projectDir, sessionID string) {
	pidPath := filepath.Join(projectDir, ".htmlgraph", "sessions", sessionID, ".collector-pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		// No PID file — collector was never spawned or already cleaned up.
		return
	}
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		debugLog(projectDir, "[session-end] collector-pid: invalid pid %q for session %s", pidStr, sessionID[:minLen(sessionID, 8)])
		return
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		// Process not found (already exited).
		return
	}

	// Send SIGTERM to request graceful drain.
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// ESRCH means process already gone — clean up PID file to prevent
		// stale PID reuse on later end/resume paths.
		_ = os.Remove(pidPath)
		return
	}
	debugLog(projectDir, "[session-end] sent SIGTERM to collector pid=%d (session=%s)", pid, sessionID[:minLen(sessionID, 8)])

	// Poll for up to 3s using kill(pid, 0) — we can't Wait() on a non-child.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break // process exited
		}
		time.Sleep(100 * time.Millisecond)
	}
	// If still alive after 3s, escalate to SIGKILL.
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		debugLog(projectDir, "[session-end] collector drain timeout — sending SIGKILL pid=%d", pid)
		_ = proc.Signal(syscall.SIGKILL)
	}

	// Remove the PID file so future SessionEnd calls don't attempt to re-signal.
	_ = os.Remove(pidPath)
}

// SessionResume handles the SessionResume Claude Code hook event.
// It updates the session status back to active and refreshes env vars.
func SessionResume(event *CloudEvent, database *sql.DB, projectDir string) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	if _, err := database.Exec(`
		UPDATE sessions
		SET status = 'active', completed_at = NULL
		WHERE session_id = ? AND status = 'completed'`,
		sessionID,
	); err != nil {
		debugLog(projectDir, "[error] handler=session-resume session=%s: update sessions: %v", sessionID[:minLen(sessionID, 8)], err)
	}

	// Re-export env vars so downstream hooks have the session ID.
	writeEnvVars(sessionID, projectDir)

	// Fetch active feature for context message.
	var featID sql.NullString
	_ = database.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&featID)

	msg := fmt.Sprintf("[HtmlGraph] Session %s resumed.", sessionID[:minLen(sessionID, 8)])
	if featID.Valid && featID.String != "" {
		msg += fmt.Sprintf(" Active feature: %s", featID.String)
	}

	return &HookResult{Continue: true, AdditionalContext: msg}, nil
}

func minLen(s string, n int) int {
	if len(s) < n {
		return len(s)
	}
	return n
}
