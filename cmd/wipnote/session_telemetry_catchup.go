package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/observe/otel/finalize"
	"github.com/shakestzd/wipnote/observe/otel/retention"
)

// catchUpTelemetry finalizes telemetry for sessions that never got a clean
// SessionEnd, so `wipnote session prune` can consider them. Dry runs skip it:
// it writes markers and session HTML.
func catchUpTelemetry(wipnoteDir, activeID string, dryRun bool) {
	if dryRun {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), telemetryCatchUpBudget)
	defer cancel()
	finalize.CatchUp(ctx, filepath.Dir(wipnoteDir), activeID)
}

// ensureTelemetryFinalized refuses to archive a session whose NDJSON has not
// been consumed. Outside a dry run it first tries to finalize the session
// itself, so an orphaned session (no clean SessionEnd) can still be archived.
func ensureTelemetryFinalized(wipnoteDir, sessionID string, dryRun bool) error {
	sessDir := retention.SessionDir(wipnoteDir, sessionID)
	eventsFile := filepath.Join(sessDir, "events.ndjson")
	if _, err := os.Stat(eventsFile); err != nil {
		return nil // nothing to consume
	}
	if retention.IndexerCaughtUp(sessDir, eventsFile) {
		return nil
	}
	if !dryRun {
		ctx, cancel := context.WithTimeout(context.Background(), telemetryCatchUpBudget)
		_, _ = finalize.Session(ctx, filepath.Dir(wipnoteDir), sessionID)
		cancel()
		if retention.IndexerCaughtUp(sessDir, eventsFile) {
			return nil
		}
	}
	return fmt.Errorf("session %q telemetry has not been fully ingested\n"+
		"Run 'wipnote session archive %s' without --dry-run, or 'wipnote prune', to finalize it first.", sessionID, sessionID)
}
