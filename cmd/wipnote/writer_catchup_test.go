package main

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedUnmarkedShard writes an aged events.ndjson with no .index-offset marker.
func seedUnmarkedShard(t *testing.T, projectRoot, id string) string {
	t.Helper()
	sessions := filepath.Join(projectRoot, ".wipnote", "sessions")
	dir := filepath.Join(sessions, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><article id="` + id + `"><h1>s</h1></article></body></html>`
	if err := os.WriteFile(filepath.Join(sessions, id+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	line := `{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"` + id +
		`-a","session_id":"` + id + `","canonical":"api_request","native":"claude_code.api_request","cost_usd":0.25}` + "\n"
	nd := filepath.Join(dir, "events.ndjson")
	if err := os.WriteFile(nd, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(nd, old, old); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dir, ".index-offset")
}

func TestTelemetryCatchUpInterval_IsTenMinutes(t *testing.T) {
	if telemetryCatchUpInterval != 10*time.Minute {
		t.Fatalf("interval = %v, want 10m", telemetryCatchUpInterval)
	}
}

// A shard with no marker gets one after the daemon catch-up loop ticks.
func TestStartTelemetryCatchUpLoop_MarksUnmarkedShard(t *testing.T) {
	root := t.TempDir()
	marker := seedUnmarkedShard(t, root, "orphan")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startTelemetryCatchUpLoop(ctx, time.Hour, root)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("startup catch-up did not write .index-offset")
}

// The loop re-runs on every tick, and stops once ctx is cancelled.
func TestStartTelemetryCatchUpLoop_TicksAndStops(t *testing.T) {
	calls := make(chan struct{}, 100)
	ctx, cancel := context.WithCancel(context.Background())
	startCatchUpLoop(ctx, 10*time.Millisecond, func(context.Context) { calls <- struct{}{} })
	for i := 0; i < 3; i++ {
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d runs, want startup + ticks", i)
		}
	}
	cancel()
	time.Sleep(100 * time.Millisecond)
	for len(calls) > 0 {
		<-calls
	}
	time.Sleep(100 * time.Millisecond)
	if len(calls) != 0 {
		t.Fatal("loop kept running after ctx cancel")
	}
}

// Two overlapping requests run catch-up once; the second is skipped.
func TestRunTelemetryCatchUp_NeverOverlaps(t *testing.T) {
	root := t.TempDir()
	catchUpMu.Lock()
	ran := runTelemetryCatchUp(context.Background(), root)
	catchUpMu.Unlock()
	if ran {
		t.Fatal("catch-up ran while another was in flight")
	}
	if !runTelemetryCatchUp(context.Background(), root) {
		t.Fatal("catch-up skipped when idle")
	}
}

// The daemon must start catch-up before its startup hydrate. The hydrate runs
// one `git log --follow` per work-item file and took over 15 minutes on a large
// repo, which left unmarked shards unfinalized that long (bug-98f32723).
func TestRunWriterOnly_CatchUpDoesNotWaitForHydrate(t *testing.T) {
	root := t.TempDir()
	marker := seedUnmarkedShard(t, root, "orphan")
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	t.Setenv("WIPNOTE_WRITER_IDLE_TIMEOUT", "200ms")

	release := make(chan struct{})
	entered := make(chan struct{})
	old := hydrateWriterDB
	hydrateWriterDB = func(context.Context, *sql.DB, string) error {
		close(entered)
		<-release
		return nil
	}
	defer func() { hydrateWriterDB = old }()

	done := make(chan error, 1)
	go func() { done <- runWriterOnly() }()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
		<-done
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("hydrate never started")
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("catch-up waited for the startup hydrate: no .index-offset while hydrate was blocked")
}
