package hooks

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/worktree"
)

// SessionEnd handles the SessionEnd Claude Code hook event.
// It records the session's end durably and closes its ledger row and claims.
//
// Cross-harness session-end coverage (feat-793844bd slice-4 part c):
//   - Claude Code: native SessionEnd event → this handler.
//   - Gemini CLI:  Stop event is mapped to geminiEventName "SessionEnd" with
//     geminiHandler "session-end" in packages/plugin-core/manifest.json, so
//     Gemini reaches this handler and releases claims on session exit.
//   - Codex CLI:   emits TaskComplete (its session-end-equivalent lifecycle
//     event); manifest.json wires TaskComplete → "session-end" for the codex
//     target, so Codex also reaches this handler.
//
// Honest liveness (db.SessionLivenessByHeartbeat) is the cross-harness safety
// net layered UNDERNEATH all three: even if a harness's session-end event
// never fires (crash, kill -9, network drop), a session whose newest claim
// heartbeat is stale is reported not-live and its lease is reaped — so
// liveness never depends on a session-end event arriving. This is why no
// invented Codex-specific hook is needed: TaskComplete already covers the
// graceful path, and heartbeat-recency + lease reap cover the abrupt path.
//
// Design (bug-1daed6d6): Claude Code gives a plugin SessionEnd hook 1.5s and
// ignores the "timeout" in hooks.json, then cancels it. So this handler does
// only what must survive: the durable end note first, then the ledger row and
// the claim episodes, then one SIGTERM to the collector. Everything else (end
// commit, handoff, lineage, features worked on, spike-worktree cleanup, the
// collector drain, the telemetry finalize) runs in CatchUpEndedSessions, which
// reads the note and is idempotent. Steps that Stop already performs
// (FinalizeSessionHTML, backfillMissedUserPrompts, runSessionExitReconcile) are
// intentionally omitted here to avoid duplicated work.
func SessionEnd(event *CloudEvent, database *sql.DB, projectDir string) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	// 1. The durable end note, before anything that can be slow. If the harness
	// cancels this hook at any later point, catch-up finishes from the note.
	RecordSessionEndNote(event, projectDir)
	note, ok := ReadSessionEndNote(projectDir, sessionID)
	if !ok {
		note = SessionEndNote{SessionID: sessionID, EndedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: event.Reason, TranscriptPath: event.TranscriptPath}
	}

	// 2. The essential writes, all local file or in-memory work: no git, no
	// full ledger scan, no waiting on another process.
	applySessionEndToProjection(database, projectDir, note)
	closeSessionClaimsAndLedger(projectDir, note)

	// 3. One SIGTERM to the collector, no wait. Draining it, the telemetry
	// finalize, the handoff and the rest run in CatchUpEndedSessions.
	termCollector(projectDir, sessionID)

	return &HookResult{Continue: true}, nil
}

func cleanupEmptySpikeWorktreeOnSessionEnd(database *sql.DB, projectDir, sessionID string) error {
	cfg := worktree.LoadCleanupConfig(projectDir)
	if !cfg.Enabled() {
		return nil
	}

	snapshot, err := loadSessionEndSnapshot(database, sessionID)
	if err != nil {
		return err
	}
	if inferTypeName(snapshot.ActiveFeatureID) != "spike" || snapshot.ExecWorktreePath == "" {
		return nil
	}
	// Guard: only clean up the worktree when its path actually belongs to
	// this spike. If the session ran in a shared track worktree (e.g.
	// ".claude/worktrees/trk-…"), the path will not contain the spike ID —
	// deleting it would destroy unrelated work.
	if !strings.Contains(snapshot.ExecWorktreePath, snapshot.ActiveFeatureID) {
		return nil
	}

	worktreePath := snapshot.ExecWorktreePath
	if !filepath.IsAbs(worktreePath) {
		worktreePath = filepath.Join(projectDir, filepath.FromSlash(worktreePath))
	}

	state, err := worktree.InspectCleanupState(projectDir, worktreePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if state.Locked {
		return nil
	}
	if !state.Removable() {
		_, snapErr := worktree.SnapshotPreservedWorktree(projectDir, worktreePath, snapshot.ActiveFeatureID, state)
		return snapErr
	}
	if !completeIfInProgress(snapshot.ActiveFeatureID, database) {
		return nil
	}
	return worktree.RemoveManagedWorktree(projectDir, worktreePath)
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

	// Refresh the session process-liveness anchor (feat-0a7db952, slice-1).
	// On resume the new launcher rewrote .launch-mode with the LIVE pid, so
	// re-resolving picks it up. Without this the resumed session would carry the
	// dead pre-resume pid and be false-reaped. Best-effort: a write failure must
	// NEVER block the hook; when NO fresh owner resolves, updateSessionPIDAnchor
	// REMOVES any stale pre-existing anchor (degrade to LIVE) rather than leaving
	// a dead pid in place.
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	updateSessionPIDAnchor(projectDir, sessDir)

	// Re-export env vars so downstream hooks have the session ID.
	writeEnvVars(sessionID, projectDir, activeSessionHarness(event))

	// Fetch active feature for context message.
	var featID sql.NullString
	_ = database.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ?`, sessionID,
	).Scan(&featID)

	msg := fmt.Sprintf("[wipnote] Session %s resumed.", sessionID[:minLen(sessionID, 8)])
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
