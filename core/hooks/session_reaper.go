package hooks

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// --- Reaper config knobs ---

// defaultReaperSessionTTLSeconds is the heartbeat-stale cutoff (in seconds) beyond
// which a session is eligible for reaping. Distinct from liveness_staleness_threshold_seconds
// (defaultLivenessStalenessSeconds = 120s in core/db): that controls the liveness
// query used by the serve-path claim reaper; this TTL controls the reaper's own
// eligibility window (30m = 1800s).
const (
	defaultReaperSessionTTLSeconds     = 1800 // 30m heartbeat-stale cutoff for reap eligibility
	defaultReaperCollectorGraceSeconds = 10   // SIGTERM→SIGKILL grace window
)

// reaperConfig decodes ONLY the reaper-specific fields from .wipnote/config.json.
// Everything else in config.json is ignored. This is the 3rd independent reader of
// config.json in this codebase (the others are livenessConfig in core/db/session_repo.go
// and readTaskCompletionConfig in core/hooks/task_completion_gate.go). There is no
// shared config package; each call site reads independently. Accepted debt, NOT a
// refactor target.
type reaperConfig struct {
	ReaperSessionTTLSeconds     int `json:"reaper_session_ttl_seconds"`
	ReaperCollectorGraceSeconds int `json:"reaper_collector_grace_seconds"`
}

// readReaperConfig loads .wipnote/config.json from projectDir and decodes the
// reaper fields. Returns a zero-value reaperConfig on projectDir=="" or any
// read/parse error; callers apply their own per-field defaults.
func readReaperConfig(projectDir string) reaperConfig {
	if projectDir == "" {
		return reaperConfig{}
	}
	var cfg reaperConfig
	if !paths.ReadProjectConfig(projectDir, &cfg) {
		return reaperConfig{}
	}
	return cfg
}

// ReaperSessionTTL returns the duration beyond which a session with a stale
// heartbeat is eligible for reaping. Reads reaper_session_ttl_seconds from
// .wipnote/config.json under projectDir; falls back to the 30m default when
// the file is missing, unreadable, unparseable, or the value is non-positive.
// projectDir=="" → default.
func ReaperSessionTTL(projectDir string) time.Duration {
	def := time.Duration(defaultReaperSessionTTLSeconds) * time.Second
	cfg := readReaperConfig(projectDir)
	if cfg.ReaperSessionTTLSeconds <= 0 {
		return def
	}
	return time.Duration(cfg.ReaperSessionTTLSeconds) * time.Second
}

// ReaperCollectorGrace returns the SIGTERM→SIGKILL grace window for the
// per-session OTel collector process during a reaper pass. Reads
// reaper_collector_grace_seconds from .wipnote/config.json under projectDir;
// falls back to the 10s default when the file is missing, unreadable,
// unparseable, or the value is non-positive. projectDir=="" → default.
func ReaperCollectorGrace(projectDir string) time.Duration {
	def := time.Duration(defaultReaperCollectorGraceSeconds) * time.Second
	cfg := readReaperConfig(projectDir)
	if cfg.ReaperCollectorGraceSeconds <= 0 {
		return def
	}
	return time.Duration(cfg.ReaperCollectorGraceSeconds) * time.Second
}

// ReapStaleSessionsAndCollectors is the idempotent, identity-verified reaper
// pass (feat-88f92f44). It transitions crashed/abandoned sessions to completed
// and (optionally) terminates orphaned per-session OTel collectors whose owning
// session is provably dead. It NEVER touches the current session and NEVER
// reaps a session it cannot positively prove dead — the safe-degrade direction
// is always "leave active".
//
// (a) Sessions: a status='active' row is reaped only when its heartbeat is
// stale (no claim heartbeat within ttl) AND its owning process is provably not
// alive (SessionReapEligible, which folds the .session-pid kill(pid,0)+
// start-time anchor). A long-IDLE but LIVE session (process alive) and a legacy
// session with no .session-pid anchor both safe-degrade to LIVE and are left
// active. Idempotency is the UPDATE's own `status='active'` guard plus a
// RowsAffected()==1 check — a concurrent pass that already reaped a row sees 0.
//
// (b) Collectors (only when includeCollectors): for every session dir carrying
// a .collector-pid, the OWNER-liveness gate fires first (never kill a live
// session's collector): skip when the owner is heartbeat-fresh OR its process
// is alive. Otherwise delegate to the injected ReapCollectorFn (observe's
// ReapCollector), which itself gates the kill on IsCollectorAlive — so a
// reused/dead collector pid is cleared without ever being signalled. A nil
// ReapCollectorFn (telemetry not wired) degrades the whole collector phase to a
// no-op.
//
// reportOnly drives the slice-3 reaper_daemon_report_only dry-run knob: when
// true, the FULL detection still runs and rep.ReapedSessions/ReapedCollectors
// are populated with the would-reap set, but NO side effects occur — the
// session UPDATE is skipped and ReapCollectorFn is never called. The config
// flag itself is NOT read here; slice 4's daemon passes the resolved bool.
//
// currentSessionID is always excluded from both phases.
//
// maxSessions caps the SESSION-reap phase: when > 0, the loop stops once that
// many sessions have been reaped (counting both real reaps and reportOnly
// would-reaps), so the synchronous hook path cannot stall behind a large stale
// backlog under write contention — the daemon catches the remainder within one
// interval. maxSessions <= 0 ⇒ unbounded (daemon callers pass 0). The cap
// applies ONLY to the session phase; the collector phase is daemon-only.
func ReapStaleSessionsAndCollectors(database *sql.DB, projectDir, currentSessionID string, includeCollectors bool, reportOnly bool, maxSessions int) *ReconcileReport {
	rep := &ReconcileReport{}
	if database == nil {
		return rep
	}
	ttl := ReaperSessionTTL(projectDir)
	now := time.Now().UTC().Format(time.RFC3339)

	// --- (0) finish sessions whose SessionEnd hook was cancelled or deferred ---
	// They hold an end note, so they close with their real end time and outcome
	// instead of falling through to the OutcomeExpired path below. maxSessions>0
	// marks the synchronous hook path, which must stay cheap (shallow).
	if !reportOnly {
		_ = CatchUpEndedSessions(database, projectDir, currentSessionID, maxSessions <= 0)
	}

	// --- (a) reap stale sessions ---
	reapStaleSessions(database, projectDir, currentSessionID, ttl, now, reportOnly, maxSessions, rep)

	// --- (b) reap orphaned collectors ---
	if includeCollectors && ReapCollectorFn != nil {
		reapOrphanCollectors(database, projectDir, currentSessionID, ttl, reportOnly, rep)
	}

	return rep
}

// reapStaleSessions is the session phase of ReapStaleSessionsAndCollectors.
func reapStaleSessions(database *sql.DB, projectDir, currentSessionID string, ttl time.Duration, now string, reportOnly bool, maxSessions int, rep *ReconcileReport) {
	sessions, err := db.ListSessions(database, true /*activeOnly*/, 1_000_000)
	if err != nil {
		debugLog(projectDir, "[reaper] list active sessions: %v", err)
		sessions = nil
	}
	reapedCount := 0
	for _, s := range sessions {
		if maxSessions > 0 && reapedCount >= maxSessions {
			// Hook-path cap reached: leave the remainder for the daemon.
			break
		}
		sid := s.SessionID
		if sid == "" || sid == currentSessionID {
			continue
		}
		heartbeatStale := !db.SessionLivenessByHeartbeat(database, sid, ttl)
		sessDir := filepath.Join(projectDir, ".wipnote", "sessions", sid)
		if !SessionReapEligible(heartbeatStale, sessDir) {
			continue
		}
		// A session already closed in the CANONICAL ledger has been reaped, by
		// this pass or an earlier one in another process. Skipping it is what
		// terminates the reap loop: the projection's own status is per-process
		// and tells the next process nothing, so without this the same dead
		// session is rediscovered and re-reaped forever.
		if sessionLedgerClosed(projectDir, sid) {
			continue
		}
		if reportOnly {
			// Dry-run: record the would-reap set; perform no UPDATE.
			rep.ReapedSessions = append(rep.ReapedSessions, sid)
			reapedCount++
			debugLog(projectDir, "[reaper] report-only: would reap stale session %s (heartbeat stale, owner process dead)", sid[:minLen(sid, 8)])
			continue
		}
		if reapSession(database, projectDir, sid, now) {
			rep.ReapedSessions = append(rep.ReapedSessions, sid)
			reapedCount++
		}
	}
}

// reapSession transitions one stale session to completed and, on a real reap,
// releases its claims and closes it in the canonical ledger. It reports
// whether this call reaped the session.
func reapSession(database *sql.DB, projectDir, sid, now string) bool {
	res, execErr := database.Exec(
		`UPDATE sessions SET status='completed', completed_at=? WHERE session_id=? AND status='active'`,
		now, sid,
	)
	if execErr != nil {
		debugLog(projectDir, "[reaper] reap session %s: %v", sid[:minLen(sid, 8)], execErr)
		return false
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return false
	}
	debugLog(projectDir, "[reaper] reaped stale session %s → completed (heartbeat stale, owner process dead)", sid[:minLen(sid, 8)])
	// Release any active claims the reaped session still held. Normal
	// SessionEnd does this (db.ReleaseAllClaimsForSession); the reaper
	// must mirror it or a crashed session's claims block claim paths
	// forever. Only on a REAL reap — never in the reportOnly branch.
	if released, relErr := db.ReleaseAllClaimsForSession(database, sid); relErr != nil {
		debugLog(projectDir, "[reaper] release claims for reaped session %s: %v", sid[:minLen(sid, 8)], relErr)
	} else if released > 0 {
		debugLog(projectDir, "[reaper] released %d claims for reaped session %s", released, sid[:minLen(sid, 8)])
	}
	// Stamp the end on the CANONICAL session ledger. Everything above
	// this line writes to the compatibility projection, which is
	// process-local — so without this the reap is invisible to the next
	// process, which rehydrates the session from the ledger as still
	// open (reindex_session_ledger.go derives status from
	// Record.IsOpen()), finds it stale, and reaps it again. This is the
	// write that makes a reap stick.
	recordSessionLedgerClose(projectDir, sid, time.Now().UTC())
	// The reaped session died without reporting anything: its episodes
	// are "expired", not "abandoned". This is the path that catches a
	// hard kill, where neither SessionEnd nor the release path ran.
	closeClaimEpisodesForSession(database, projectDir, sid, claimledger.OutcomeExpired)
	return true
}

// reapOrphanCollectors is the collector phase of
// ReapStaleSessionsAndCollectors.
func reapOrphanCollectors(database *sql.DB, projectDir, currentSessionID string, ttl time.Duration, reportOnly bool, rep *ReconcileReport) {
	grace := ReaperCollectorGrace(projectDir)
	// os.ReadDir (not filepath.Glob) so session ids / paths containing glob
	// metacharacters ([ * ?) are scanned correctly — Glob would silently
	// skip or mis-match them.
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	entries, _ := os.ReadDir(sessionsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sid := e.Name()
		sessDir := filepath.Join(sessionsDir, sid)
		if sid == "" || sid == currentSessionID {
			continue
		}
		if _, statErr := os.Stat(filepath.Join(sessDir, ".collector-pid")); statErr != nil {
			continue // no collector record for this session
		}
		// Owner-liveness gate: never kill a live session's collector.
		heartbeatStale := !db.SessionLivenessByHeartbeat(database, sid, ttl)
		if !heartbeatStale || IsSessionProcessAlive(sessDir) {
			continue
		}
		if reportOnly {
			rep.ReapedCollectors = append(rep.ReapedCollectors, sid)
			debugLog(projectDir, "[reaper] report-only: would reap orphan collector for session %s", sid[:minLen(sid, 8)])
			continue
		}
		pid, reaped := ReapCollectorFn(sessDir, grace)
		if reaped {
			rep.ReapedCollectors = append(rep.ReapedCollectors, fmt.Sprintf("%s:%d", sid, pid))
			debugLog(projectDir, "[reaper] reaped orphan collector pid=%d for session %s", pid, sid[:minLen(sid, 8)])
		}
		// !reaped ⇒ ReapCollector cleared a dead/reused record; not a kill.
	}
}

// sessionLedgerClosed reports whether the canonical session ledger already
// records an end for sessionID. A missing row answers false — legacy sessions
// that predate the ledger must stay reapable, and a row that does not exist
// cannot be the source of a repeat-reap loop anyway.
func sessionLedgerClosed(projectDir, sessionID string) bool {
	if projectDir == "" || sessionID == "" {
		return false
	}
	rec, found, err := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote")).Get(sessionID)
	if err != nil || !found {
		return false
	}
	return !rec.IsOpen()
}
