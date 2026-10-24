package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sqls "github.com/shakestzd/wipnote/observe/otel/sink/sqlite"
)

var replayFixture = []string{
	`{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"r1","session_id":"replay-sess","canonical":"api_request","native":"claude_code.api_request","cost_usd":0.25}`,
	`{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:01Z","signal_id":"r2","session_id":"replay-sess","canonical":"api_request","native":"claude_code.api_request","cost_usd":0.5}`,
	`{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:02Z","signal_id":"r3","session_id":"replay-sess","canonical":"tool_result","native":"claude_code.tool_result"}`,
}

// ReplaySession reads the whole log from byte 0 even when a stale marker
// claims it was consumed: the destination projection starts empty, so the
// marker is never a resume point. It also leaves the marker untouched, since
// the caller publishes it only after the rollup is written.
func TestReplaySession_ReadsFromZeroAndLeavesMarker(t *testing.T) {
	w, _ := setupIndexerDB(t)
	wipnoteDir := t.TempDir()
	path := writeNDJSONFixture(t, wipnoteDir, "replay-sess", replayFixture)
	marker := filepath.Join(filepath.Dir(path), ".index-offset")
	if err := os.WriteFile(marker, []byte("999999"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := New(wipnoteDir, sqls.New(w)).ReplaySession(context.Background(), "replay-sess")
	if err != nil {
		t.Fatalf("ReplaySession: %v", err)
	}
	info, _ := os.Stat(path)
	if !res.Complete || res.Offset != info.Size() {
		t.Fatalf("result = %+v, want complete at offset %d", res, info.Size())
	}
	if got := countSignals(t, w, "replay-sess"); got != 3 {
		t.Fatalf("signals = %d, want 3", got)
	}
	if data, _ := os.ReadFile(marker); string(data) != "999999" {
		t.Fatalf("marker rewritten to %q; ReplaySession must not publish it", data)
	}
}

// A spent budget stops the replay before EOF and reports it incomplete, so
// the caller neither materialises a partial rollup nor marks the log consumed.
func TestReplaySession_ExpiredContextIsIncomplete(t *testing.T) {
	w, _ := setupIndexerDB(t)
	wipnoteDir := t.TempDir()
	writeNDJSONFixture(t, wipnoteDir, "replay-sess", replayFixture)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := New(wipnoteDir, sqls.New(w)).ReplaySession(ctx, "replay-sess")
	if err != nil {
		t.Fatalf("ReplaySession: %v", err)
	}
	if res.Complete {
		t.Fatalf("result = %+v, want incomplete", res)
	}
}

// An unterminated final line (the collector died mid-write) still counts as
// consumed: the log is final, so the marker must be able to reach its size.
func TestReplaySession_UnterminatedTailCompletes(t *testing.T) {
	w, _ := setupIndexerDB(t)
	wipnoteDir := t.TempDir()
	path := writeNDJSONFixture(t, wipnoteDir, "replay-sess", replayFixture[:2])
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(`{"kind":"log","harness":"claude_co`)
	_ = f.Close()

	res, err := New(wipnoteDir, sqls.New(w)).ReplaySession(context.Background(), "replay-sess")
	if err != nil {
		t.Fatalf("ReplaySession: %v", err)
	}
	info, _ := os.Stat(path)
	if !res.Complete || res.Offset != info.Size() {
		t.Fatalf("result = %+v, want complete at offset %d", res, info.Size())
	}
}
