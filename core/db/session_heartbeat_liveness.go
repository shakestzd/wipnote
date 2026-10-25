package db

import (
	"database/sql"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// defaultLivenessStalenessSeconds is 2× claimHeartbeatInterval. A session
// whose newest claim heartbeat is older than this is NOT live, regardless of
// sessions.status (folds bug-6c3e8252: stale status='active' ghost rows).
const defaultLivenessStalenessSeconds = 120

// livenessConfig mirrors the local os.ReadFile(.wipnote/config.json) pattern
// used by ReadSpecEnforcement in core/hooks/spec_enforcement.go
// (there is no shared internal/config package). Only the one tunable field is
// decoded; everything else in config.json is ignored.
type livenessConfig struct {
	LivenessStalenessSeconds int `json:"liveness_staleness_seconds"`
}

// LivenessStalenessThreshold returns the heartbeat-age cutoff beyond which a
// session is considered not-live. Reads .wipnote/config.json under projectDir;
// falls back to the 2×interval default when the file is missing, unreadable,
// the key is absent, or the value is non-positive. projectDir may be "" (e.g.
// CLI contexts without a resolved project) — the default is returned then.
func LivenessStalenessThreshold(projectDir string) time.Duration {
	def := time.Duration(defaultLivenessStalenessSeconds) * time.Second
	if projectDir == "" {
		return def
	}
	var cfg livenessConfig
	if !paths.ReadProjectConfig(projectDir, &cfg) {
		return def
	}
	if cfg.LivenessStalenessSeconds <= 0 {
		return def
	}
	return time.Duration(cfg.LivenessStalenessSeconds) * time.Second
}

// SessionLivenessByHeartbeat reports whether a session is *honestly* live,
// derived from claim-heartbeat recency rather than sessions.status. This is the
// cross-harness liveness primitive: it works for every harness with zero
// dependency on a session-end event firing (folds bug-6c3e8252 — stale
// status='active' rows whose last heartbeat is ancient are correctly reported
// not-live).
//
// A session is live iff it has at least one claim whose last_heartbeat_at is
// within `threshold` of now. Sessions with no claims at all are not live (we
// have no liveness signal for them — honest absence of evidence). The query is
// a single indexed lookup on claims(owner_session_id); it never writes.
func SessionLivenessByHeartbeat(db *sql.DB, sessionID string, threshold time.Duration) bool {
	if db == nil || sessionID == "" {
		return false
	}
	var hb sql.NullString
	err := db.QueryRow(`
		SELECT MAX(last_heartbeat_at) FROM claims
		WHERE owner_session_id = ?`, sessionID).Scan(&hb)
	if err != nil || !hb.Valid || hb.String == "" {
		return false
	}
	t, perr := time.Parse(time.RFC3339, hb.String)
	if perr != nil {
		return false
	}
	return time.Since(t) <= threshold
}
