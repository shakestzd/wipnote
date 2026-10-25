package finalize

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/observe/otel/materialize"
	"github.com/shakestzd/wipnote/observe/otel/retention"
)

// ShardsFor lists the telemetry shard directories a SessionEnd hook should
// finalize. A shard is named for the launcher's OTel session id
// (WIPNOTE_OTEL_SESSION_ID, the id `wipnote claude` passes to otel-collect),
// not for the harness session id the hook receives. launcherID is that env
// value. The harness id's own directory is kept too, for sessions whose
// collector was keyed by the harness id. Only shards with an events.ndjson are
// returned, each once, launcher shard first.
func ShardsFor(projectDir, sessionID, launcherID string) []string {
	var out []string
	for _, id := range []string{launcherID, sessionID} {
		if id == "" || retention.ValidateSessionID(id) != nil || contains(out, id) {
			continue
		}
		ndjson := filepath.Join(projectDir, ".wipnote", "sessions", id, "events.ndjson")
		if _, err := os.Stat(ndjson); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func contains(ids []string, id string) bool {
	for _, have := range ids {
		if have == id {
			return true
		}
	}
	return false
}

// materializeShard rolls up every session id the shard's signals carry. The
// shard directory name is the launcher id, but each signal's session_id is the
// harness id, and one shard can hold several (a resume keeps the harness id
// under a new shard). The shard name is rolled up as well, for collectors
// keyed by the harness id.
func materializeShard(database *sql.DB, projectDir, shard string) error {
	ids, err := signalSessionIDs(database)
	if err != nil {
		return err
	}
	if !contains(ids, shard) {
		ids = append(ids, shard)
	}
	var errs []error
	for _, id := range ids {
		if err := materialize.Materialize(database, projectDir, id); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// signalSessionIDs returns the distinct session ids in the projection.
func signalSessionIDs(database *sql.DB) ([]string, error) {
	rows, err := database.Query(`SELECT DISTINCT session_id FROM otel_signals WHERE session_id != ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if retention.ValidateSessionID(id) == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
