package hooks

import (
	"database/sql"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/fsprobe"
)

// This file holds the writes SessionEnd makes INSIDE the harness's window
// (Claude Code: 1.5s, "Timeouts set on plugin-provided hooks don't raise the
// budget"). Every function here spawns no process and scans no claim shard.

// applySessionEndToProjection marks the session completed in the projection and
// releases its claim rows there. Both are in-memory, so they cost microseconds;
// end_commit is left to catch-up because it needs a git spawn.
func applySessionEndToProjection(database *sql.DB, projectDir string, note SessionEndNote) {
	if database == nil {
		return
	}
	sid := note.SessionID
	completedAt := note.EndTime().Format("2006-01-02T15:04:05Z07:00")
	if _, err := database.Exec(`
		UPDATE sessions
		SET status = 'completed', completed_at = ?
		WHERE session_id = ?`, completedAt, sid); err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: update sessions: %v", sid[:minLen(sid, 8)], err)
	}
	if note.TranscriptPath != "" || note.Reason != "" {
		_, _ = database.Exec(`
			UPDATE sessions
			SET transcript_path = COALESCE(NULLIF(?, ''), transcript_path),
			    metadata = json_set(COALESCE(metadata, '{}'), '$.end_reason', ?)
			WHERE session_id = ?`, note.TranscriptPath, note.Reason, sid)
	}
	if _, err := db.ReleaseAllClaimsForSession(database, sid); err != nil {
		debugLog(projectDir, "[error] handler=session-end session=%s: release claims: %v", sid[:minLen(sid, 8)], err)
	}
}

// closeSessionClaimsAndLedger stamps the end on the session's ledger row and
// closes its open claim episodes, using the END TIME AND OUTCOME FROM THE NOTE.
// Catch-up calls it too, so a cancelled hook and a finished one record the same
// interval. Returns whether any candidate claim shard existed.
func closeSessionClaimsAndLedger(projectDir string, note SessionEndNote) bool {
	end := note.EndTime()
	recordSessionLedgerClose(projectDir, note.SessionID, end)
	return closeClaimEpisodesCheap(projectDir, note.SessionID, note.ClaimOutcome(), end)
}

// cheapClaimRoots lists the root sessions whose shard may hold sessionID's
// episodes, without reading the ledger: the session itself (under Claude Code
// subagents share the root's id, so this is the usual answer), the exported
// parent, and the recorded session-family root.
func cheapClaimRoots(projectDir, sessionID string) []string {
	roots := []string{sessionID}
	add := func(r string) {
		if r == "" {
			return
		}
		for _, have := range roots {
			if have == r {
				return
			}
		}
		roots = append(roots, r)
	}
	add(os.Getenv("WIPNOTE_PARENT_SESSION"))
	add(agent.SessionFamilyFor(projectDir, sessionID))
	return roots
}

// closeClaimEpisodesCheap closes sessionID's open episodes in each candidate
// root shard that exists. A shard that does not exist costs one stat and no
// read. Closing is scoped to this session's rows, never its siblings'.
func closeClaimEpisodesCheap(projectDir, sessionID string, outcome claimledger.Outcome, endedAt time.Time) bool {
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	store := claimledger.NewStore(wipnoteDir)
	found := false
	for _, root := range cheapClaimRoots(projectDir, sessionID) {
		if !claimShardExists(wipnoteDir, root) {
			continue
		}
		found = true
		n, err := store.CloseAllForSession(root, sessionID, outcome, endedAt)
		if err != nil {
			debugLog(projectDir, "[claim-ledger] close episodes for %s: %v", sessionID[:minLen(sessionID, 8)], err)
			continue
		}
		if n > 0 {
			debugLog(projectDir, "[claim-ledger] closed %d episode(s) as %s for session %s", n, outcome, sessionID[:minLen(sessionID, 8)])
		}
	}
	return found
}

// termCollector sends the session's collector one SIGTERM and moves on. It does
// not wait, does not SIGKILL and leaves the pid file: the collector may ignore
// SIGTERM, and a hook has 1.5s. CatchUpEndedSessions drains or kills it later.
func termCollector(projectDir, sessionID string) {
	signalCollectorDirWait(projectDir, sessionID, 0, false)
	if launcher := os.Getenv("WIPNOTE_OTEL_SESSION_ID"); launcher != "" && launcher != sessionID &&
		filepath.Base(launcher) == launcher && launcher != ".." && launcher != "." {
		signalCollectorDirWait(projectDir, launcher, 0, false)
	}
}

// claimShardExists reports whether root has a claim shard, as one stat.
func claimShardExists(wipnoteDir, root string) bool {
	path := claimledger.NewStore(wipnoteDir).ShardPath(root)
	fsprobe.Touch(fsprobe.OpRead, path)
	_, err := os.Stat(path)
	return err == nil
}
