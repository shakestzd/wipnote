package finalize

import (
	"context"
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/retention"
)

// CatchUpResult counts what one CatchUp pass did.
type CatchUpResult struct {
	Stale      int // sessions found eligible: quiet and not yet marked
	Finalized  int // sessions replayed, rolled up and marked
	Incomplete int // sessions the budget ran out on; retried next pass
	Failed     int // sessions that returned an error
}

// staleSession is a session whose log is quiet and not yet marked consumed.
type staleSession struct {
	id    string
	mtime time.Time
}

// CatchUp finalizes every session whose telemetry was never marked consumed:
// sessions that crashed, had their pane closed, or had SessionEnd killed by a
// harness cap before it finished. It skips activeSessionID and any log written
// within retention.ActiveGrace, so it never finalizes a log still being
// appended. Oldest sessions go first, and the pass stops when ctx expires.
func CatchUp(ctx context.Context, projectDir, activeSessionID string) CatchUpResult {
	var res CatchUpResult
	stale := staleSessions(projectDir, activeSessionID, time.Now())
	res.Stale = len(stale)
	for _, s := range stale {
		if ctx.Err() != nil {
			break
		}
		out, err := Session(ctx, projectDir, s.id)
		switch {
		case err != nil:
			log.Printf("telemetry catch-up: session %s: %v", s.id, err)
			res.Failed++
		case out.Complete:
			res.Finalized++
		default:
			res.Incomplete++
			log.Printf("telemetry catch-up: session %s incomplete: offset=%d size=%d", s.id, out.Offset, out.Size)
		}
	}
	return res
}

// staleSessions lists sessions eligible for catch-up, oldest first.
func staleSessions(projectDir, activeSessionID string, now time.Time) []staleSession {
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var out []staleSession
	for _, e := range entries {
		if !e.IsDir() || e.Name() == activeSessionID {
			continue
		}
		sessDir := filepath.Join(sessionsDir, e.Name())
		events := filepath.Join(sessDir, "events.ndjson")
		info, err := os.Stat(events)
		if err != nil || now.Sub(info.ModTime()) < retention.ActiveGrace() {
			continue
		}
		if retention.IndexerCaughtUp(sessDir, events) {
			continue
		}
		out = append(out, staleSession{id: e.Name(), mtime: info.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.Before(out[j].mtime) })
	return out
}

// Sweep runs a telemetry catch-up pass bounded by budget, then the retention
// sweep. Catch-up goes first so sessions it marks can be archived in the same
// pass. Dry runs skip catch-up: it writes markers and session HTML, and a dry
// run promises to change nothing.
func Sweep(database *sql.DB, wipnoteDir, activeSessionID string, dryRun bool, budget time.Duration) (retention.SweepResult, error) {
	if !dryRun {
		ctx, cancel := context.WithTimeout(context.Background(), budget)
		CatchUp(ctx, filepath.Dir(wipnoteDir), activeSessionID)
		cancel()
	}
	return retention.Sweep(database, wipnoteDir, activeSessionID, dryRun)
}
