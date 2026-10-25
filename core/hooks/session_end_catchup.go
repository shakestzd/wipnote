package hooks

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/paths"
)

// Catch-up stages recorded in the done marker.
const (
	stageEssential = "essential" // ledger, episodes, projection, handoff
	stageFull      = "full"      // plus end commit, spike cleanup, collector, telemetry
)

// catchUpTelemetryBudget bounds the telemetry finalize a catch-up pass runs. It
// is deliberately short: the retention catch-up finishes any session it misses.
const catchUpTelemetryBudget = 2 * time.Second

// collectorKillWait is how long catch-up waits after SIGTERM before SIGKILL.
const collectorKillWait = 300 * time.Millisecond

type catchUpMarker struct {
	Stage   string `json:"stage"`
	EndedAt string `json:"ended_at"`
}

func readCatchUpMarker(sessDir string) catchUpMarker {
	var m catchUpMarker
	if data, err := os.ReadFile(filepath.Join(sessDir, sessionEndDoneFile)); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func writeCatchUpMarker(sessDir string, m catchUpMarker) {
	if data, err := json.Marshal(m); err == nil {
		_ = os.WriteFile(filepath.Join(sessDir, sessionEndDoneFile), data, 0o644)
	}
}

// done reports whether the marker already covers stage for this exact end.
// A resumed session that ends again writes a note with a new ended_at, which
// makes the old marker stale.
func (m catchUpMarker) done(note SessionEndNote, deep bool) bool {
	if m.EndedAt != note.EndedAt {
		return false
	}
	return m.Stage == stageFull || (!deep && m.Stage == stageEssential)
}

// CatchUpEndedSessions finishes the work of SessionEnd hooks that the harness
// cancelled, or that deliberately deferred it. It reads each session's end
// note and closes whatever is still open, using the note's end time and
// outcome (never OutcomeExpired: the session told us it ended).
//
// Idempotent: the ledger refuses to move a recorded end, closing episodes only
// touches open ones, and a done marker per note skips finished sessions.
//
// deep == false is the session-start path: it must stay cheap, so it does the
// essential stage only (no git, no shard scan, no telemetry). deep == true is
// the daemon/CLI path and adds the end commit, spike-worktree cleanup, the
// collector drain/kill, the telemetry finalize and the full-ledger fallback for
// a session whose root shard the cheap lookup could not find.
//
// currentSessionID is never touched. Returns the sessions it finished.
func CatchUpEndedSessions(database *sql.DB, projectDir, currentSessionID string, deep bool) []string {
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var finished []string
	for _, e := range entries {
		sid := e.Name()
		if !e.IsDir() || sid == currentSessionID {
			continue
		}
		sessDir := filepath.Join(sessionsDir, sid)
		if _, err := os.Stat(filepath.Join(sessDir, SessionEndNoteFile)); err != nil {
			continue
		}
		note, ok := ReadSessionEndNote(projectDir, sid)
		if !ok || readCatchUpMarker(sessDir).done(note, deep) {
			continue
		}
		finishEndedSession(database, projectDir, sessDir, note, deep)
		finished = append(finished, sid)
	}
	return finished
}

func finishEndedSession(database *sql.DB, projectDir, sessDir string, note SessionEndNote, deep bool) {
	sid := note.SessionID
	foundShard := closeSessionClaimsAndLedger(projectDir, note)
	applySessionEndToProjection(database, projectDir, note)
	finishDeferredProjectionWork(database, projectDir, note)
	paths.CleanupSessionHint(sid)
	stage := stageEssential
	if deep {
		if !foundShard {
			closeClaimEpisodesByLedgerScan(projectDir, note)
		}
		finishDeepWork(database, projectDir, note)
		stage = stageFull
	}
	writeCatchUpMarker(sessDir, catchUpMarker{Stage: stage, EndedAt: note.EndedAt})
}

// finishDeferredProjectionWork runs the projection writes the hook used to make
// inline: features worked on, the structured handoff, the lineage trace.
func finishDeferredProjectionWork(database *sql.DB, projectDir string, note SessionEndNote) {
	if database == nil {
		return
	}
	sid := note.SessionID
	var featuresWorkedOn []string
	if feats, err := db.DistinctFeatureIDs(database, sid); err == nil && len(feats) > 0 {
		featuresWorkedOn = feats
		if featsJSON, jErr := json.Marshal(feats); jErr == nil {
			_, _ = database.Exec(`UPDATE sessions SET features_worked_on = ? WHERE session_id = ?`, string(featsJSON), sid)
		}
	}
	evt := &CloudEvent{SessionID: sid, Reason: note.Reason, TranscriptPath: note.TranscriptPath}
	if err := persistSessionEndHandoff(database, sid, evt, projectDir, featuresWorkedOn); err != nil {
		debugLog(projectDir, "[error] catch-up session=%s: persist handoff: %v", sid[:minLen(sid, 8)], err)
	}
	if err := db.CompleteLineageTrace(database, sid); err != nil {
		debugLog(projectDir, "[error] catch-up session=%s: complete lineage trace: %v", sid[:minLen(sid, 8)], err)
	}
}

// finishDeepWork runs the steps that need git, a wait, or a long scan.
func finishDeepWork(database *sql.DB, projectDir string, note SessionEndNote) {
	sid := note.SessionID
	if database != nil {
		if commit := headCommit(projectDir); commit != "" {
			_, _ = database.Exec(`UPDATE sessions SET end_commit = COALESCE(NULLIF(end_commit, ''), ?) WHERE session_id = ?`, commit, sid)
		}
		if err := cleanupEmptySpikeWorktreeOnSessionEnd(database, projectDir, sid); err != nil {
			debugLog(projectDir, "[error] catch-up session=%s: cleanup empty spike worktree: %v", sid[:minLen(sid, 8)], err)
		}
	}
	drainEndedCollector(projectDir, sid)
	if SessionTelemetryFn != nil {
		if err := SessionTelemetryFn(projectDir, sid, catchUpTelemetryBudget); err != nil {
			debugLog(projectDir, "[error] catch-up session=%s: finalize telemetry: %v", sid[:minLen(sid, 8)], err)
		}
	}
}

// drainEndedCollector gives a collector the hook only SIGTERMed a short wait,
// then SIGKILLs it, for the launcher's shard id and the harness id alike.
func drainEndedCollector(projectDir, sessionID string) {
	if launcher := os.Getenv("WIPNOTE_OTEL_SESSION_ID"); launcher != "" && launcher != sessionID &&
		filepath.Base(launcher) == launcher && launcher != ".." && launcher != "." {
		signalCollectorDirWait(projectDir, launcher, collectorKillWait, true)
	}
	signalCollectorDirWait(projectDir, sessionID, collectorKillWait, true)
}

// closeClaimEpisodesByLedgerScan is the ReadAll fallback for a session whose
// root shard the cheap lookup could not name. It runs only in catch-up, never
// in the hook window.
func closeClaimEpisodesByLedgerScan(projectDir string, note SessionEndNote) {
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	root := rootSessionForClaimLedger(wipnoteDir, note.SessionID)
	if root == "" {
		return
	}
	_, err := claimledger.NewStore(wipnoteDir).CloseAllForSession(root, note.SessionID, note.ClaimOutcome(), note.EndTime())
	if err != nil {
		debugLog(projectDir, "[claim-ledger] catch-up close for %s: %v", note.SessionID[:minLen(note.SessionID, 8)], err)
	}
}
