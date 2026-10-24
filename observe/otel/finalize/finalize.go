// Package finalize turns a session's raw collector telemetry into its durable
// result and then marks the raw log consumed. It replaces the dashboard's
// background indexer, which the removed dashboard server used to run.
//
// For one session, Session does three steps in this order:
//
//  1. replay the session's events.ndjson into a private in-memory projection
//     (the same indexer path `wipnote reindex` uses);
//  2. materialize the OTel rollup from that projection into otel_session_rollup
//     and the session's canonical HTML;
//  3. publish .index-offset, which authorises retention and prune to archive
//     the raw log.
//
// The order makes the marker true: it never claims bytes that were not read,
// and it is never written before the rollup that depends on those bytes. If
// the budget runs out first, nothing is written and the session stays
// "not caught up". CatchUp then finishes the job for every such session, which
// also covers sessions that never got a clean SessionEnd.
package finalize

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/observe/otel/indexer"
	"github.com/shakestzd/wipnote/observe/otel/materialize"
	"github.com/shakestzd/wipnote/observe/otel/receiver"
	sqls "github.com/shakestzd/wipnote/observe/otel/sink/sqlite"
)

// Session finalizes one session's telemetry within ctx's deadline. It returns
// the replay result; Complete=false means the budget ran out (or there was no
// NDJSON) and nothing was written.
//
// It uses its own projection rather than the caller's handle. The projection
// then holds exactly this session's signals, and a read-only or shared hook
// handle can never block the write.
//
// A materialize error is returned but does not withhold the marker: the raw
// log was fully read, and archiving keeps it recoverable as a tarball. Holding
// the marker back would block retention forever on a malformed session HTML.
func Session(ctx context.Context, projectDir, sessionID string) (indexer.ReplayResult, error) {
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	ndjson := filepath.Join(wipnoteDir, "sessions", sessionID, "events.ndjson")
	if _, err := os.Stat(ndjson); errors.Is(err, os.ErrNotExist) {
		return indexer.ReplayResult{}, nil
	}

	database, err := db.OpenEphemeralProjection()
	if err != nil {
		return indexer.ReplayResult{}, err
	}
	defer database.Close()

	res, err := replay(ctx, database, wipnoteDir, sessionID)
	if err != nil || !res.Complete {
		return res, err
	}
	matErr := materialize.Materialize(database, projectDir, sessionID)
	marker := filepath.Join(filepath.Dir(ndjson), ".index-offset")
	if err := indexer.PublishProgress(marker, res.Offset); err != nil {
		return res, errors.Join(matErr, err)
	}
	if matErr != nil {
		return res, fmt.Errorf("materialize: %w", matErr)
	}
	return res, nil
}

// replay drives the indexer over one session into database.
func replay(ctx context.Context, database *sql.DB, wipnoteDir, sessionID string) (indexer.ReplayResult, error) {
	writer, err := receiver.NewWriterFromDB(database)
	if err != nil {
		return indexer.ReplayResult{}, err
	}
	idx := indexer.New(wipnoteDir, sqls.New(writer)).WithWriteDB(database)
	return idx.ReplaySession(ctx, sessionID)
}
