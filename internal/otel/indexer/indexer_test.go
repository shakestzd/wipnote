package indexer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/erinn/internal/db"
	"github.com/shakestzd/erinn/internal/otel"
	"github.com/shakestzd/erinn/internal/otel/receiver"
	sqls "github.com/shakestzd/erinn/internal/otel/sink/sqlite"
)

// setupIndexerDB creates a temporary SQLite DB with OTel schema and returns writer + db path.
func setupIndexerDB(t *testing.T) (*receiver.Writer, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "otel.db")
	readDB, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	readDB.Close()
	w, err := receiver.NewWriter(dbPath)
	if err != nil {
		t.Fatalf("receiver.NewWriter: %v", err)
	}
	t.Cleanup(func() { w.Close() })
	return w, dbPath
}

// writeNDJSONFixture creates a .htmlgraph/sessions/<sid>/events.ndjson file
// with the given NDJSON lines.
func writeNDJSONFixture(t *testing.T, htmlgraphDir, sessionID string, lines []string) string {
	t.Helper()
	sessDir := filepath.Join(htmlgraphDir, "sessions", sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ndjsonPath := filepath.Join(sessDir, "events.ndjson")
	var content []byte
	for _, l := range lines {
		content = append(content, []byte(l+"\n")...)
	}
	if err := os.WriteFile(ndjsonPath, content, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return ndjsonPath
}

// countSignals returns the number of rows in otel_signals for the given session.
func countSignals(t *testing.T, w *receiver.Writer, sessionID string) int {
	t.Helper()
	var n int
	row := w.DB().QueryRow(`SELECT COUNT(*) FROM otel_signals WHERE session_id = ?`, sessionID)
	if err := row.Scan(&n); err != nil {
		t.Fatalf("count signals: %v", err)
	}
	return n
}

// TestIndexer_ProcessesNDJSONFile verifies the indexer applies all signal lines from
// a pre-written NDJSON file to the SQLite database.
func TestIndexer_ProcessesNDJSONFile(t *testing.T) {
	w, _ := setupIndexerDB(t)
	htmlgraphDir := t.TempDir()
	sessionID := "idx-test-sess-01"

	lines := []string{
		`{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"s1","session_id":"idx-test-sess-01","canonical":"api_request","native":"claude_code.api_request"}`,
		`{"kind":"metric","harness":"claude_code","ts":"2026-04-24T19:00:01Z","signal_id":"s2","session_id":"idx-test-sess-01","canonical":"token_usage","native":"claude_code.token_usage","tokens_input":100,"tokens_output":50}`,
		`{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:02Z","signal_id":"s3","session_id":"idx-test-sess-01","canonical":"session_start","native":"claude_code.session.start"}`,
	}
	writeNDJSONFixture(t, htmlgraphDir, sessionID, lines)

	snk := sqls.New(w)
	idxr := New(htmlgraphDir, snk)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("processSession: %v", err)
	}

	got := countSignals(t, w, sessionID)
	if got != 3 {
		t.Errorf("want 3 signals in DB, got %d", got)
	}
}

// TestIndexer_IdempotentReplay verifies that replaying the same NDJSON file twice
// doesn't create duplicate rows (INSERT OR IGNORE on signal_id).
func TestIndexer_IdempotentReplay(t *testing.T) {
	w, _ := setupIndexerDB(t)
	htmlgraphDir := t.TempDir()
	sessionID := "idx-test-sess-02"

	lines := []string{
		`{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"dup-s1","session_id":"idx-test-sess-02","canonical":"api_request","native":"claude_code.api_request"}`,
	}
	writeNDJSONFixture(t, htmlgraphDir, sessionID, lines)

	snk := sqls.New(w)
	idxr := New(htmlgraphDir, snk)
	ctx := context.Background()

	// First replay.
	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("first processSession: %v", err)
	}
	// Reset checkpoint to force full replay.
	checkpointPath := filepath.Join(htmlgraphDir, "sessions", sessionID, ".index-offset")
	if err := os.Remove(checkpointPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("remove checkpoint: %v", err)
	}

	// Second replay from offset 0.
	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("second processSession: %v", err)
	}

	got := countSignals(t, w, sessionID)
	if got != 1 {
		t.Errorf("want 1 signal (idempotent), got %d", got)
	}
}

// TestIndexer_SkipsCollectorStart verifies that collector_start lines don't create DB rows.
func TestIndexer_SkipsCollectorStart(t *testing.T) {
	w, _ := setupIndexerDB(t)
	htmlgraphDir := t.TempDir()
	sessionID := "idx-test-sess-03"

	lines := []string{
		`{"kind":"collector_start","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"cs-1","session_id":"idx-test-sess-03"}`,
		`{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:01Z","signal_id":"real-s1","session_id":"idx-test-sess-03","canonical":"api_request","native":"claude_code.api_request"}`,
	}
	writeNDJSONFixture(t, htmlgraphDir, sessionID, lines)

	snk := sqls.New(w)
	idxr := New(htmlgraphDir, snk)
	ctx := context.Background()

	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("processSession: %v", err)
	}

	got := countSignals(t, w, sessionID)
	if got != 1 {
		t.Errorf("want 1 signal (skipped collector_start), got %d", got)
	}
}

// TestIndexer_CheckpointResumesFromOffset verifies that the indexer only processes
// new lines after the checkpoint offset.
func TestIndexer_CheckpointResumesFromOffset(t *testing.T) {
	w, _ := setupIndexerDB(t)
	htmlgraphDir := t.TempDir()
	sessionID := "idx-test-sess-04"

	line1 := `{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"chk-s1","session_id":"idx-test-sess-04","canonical":"api_request","native":"claude_code.api_request"}`
	line2 := `{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:01Z","signal_id":"chk-s2","session_id":"idx-test-sess-04","canonical":"api_request","native":"claude_code.api_request"}`

	ndjsonPath := writeNDJSONFixture(t, htmlgraphDir, sessionID, []string{line1})

	snk := sqls.New(w)
	idxr := New(htmlgraphDir, snk)
	ctx := context.Background()

	// First run: processes line1, checkpoints after it.
	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("first processSession: %v", err)
	}

	// Append line2 to the file.
	f, err := os.OpenFile(ndjsonPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open for append: %v", err)
	}
	if _, err := f.WriteString(line2 + "\n"); err != nil {
		f.Close()
		t.Fatalf("append: %v", err)
	}
	f.Close()

	// Second run: should only process line2 (line1 already checkpointed).
	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("second processSession: %v", err)
	}

	got := countSignals(t, w, sessionID)
	if got != 2 {
		t.Errorf("want 2 signals total (1 per run), got %d", got)
	}
}

// TestIndexer_Status verifies the status snapshot reflects per-session offsets and sizes.
func TestIndexer_Status(t *testing.T) {
	w, _ := setupIndexerDB(t)
	htmlgraphDir := t.TempDir()
	sessionID := "idx-status-sess"

	lines := []string{
		`{"kind":"span","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"st-s1","session_id":"idx-status-sess","canonical":"api_request","native":"claude_code.api_request"}`,
	}
	writeNDJSONFixture(t, htmlgraphDir, sessionID, lines)

	snk := sqls.New(w)
	idxr := New(htmlgraphDir, snk)
	ctx := context.Background()

	if err := idxr.processSession(ctx, sessionID); err != nil {
		t.Fatalf("processSession: %v", err)
	}

	status := idxr.Status()
	if len(status) == 0 {
		t.Fatal("Status() returned empty map")
	}

	fi, ok := status[sessionID]
	if !ok {
		t.Fatalf("status map missing session %q, got keys: %v", sessionID, statusKeys(status))
	}
	if fi.LastOffset <= 0 {
		t.Errorf("LastOffset should be > 0 after indexing, got %d", fi.LastOffset)
	}
	if fi.CurrentSize <= 0 {
		t.Errorf("CurrentSize should be > 0, got %d", fi.CurrentSize)
	}
}

// TestIndexer_Start_ContextCancel verifies Start() respects context cancellation.
func TestIndexer_Start_ContextCancel(t *testing.T) {
	htmlgraphDir := t.TempDir()
	// Create the sessions dir so the indexer can scan it.
	_ = os.MkdirAll(filepath.Join(htmlgraphDir, "sessions"), 0o755)

	// Use a fakeWriter that satisfies sqls.WriterCloser.
	snk := sqls.New(&fakeWriter{})
	idxr := New(htmlgraphDir, snk)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		idxr.Start(ctx)
		close(done)
	}()

	// Cancel quickly; Start should return.
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Start() did not return after context cancel")
	}
}

// fakeWriter satisfies sqls.WriterCloser for tests that don't need real DB.
type fakeWriter struct{}

func (f *fakeWriter) WriteBatch(_ context.Context, _ otel.Harness, _ map[string]any, signals []otel.UnifiedSignal) (int, error) {
	return len(signals), nil
}
func (f *fakeWriter) Close() error { return nil }

// statusKeys returns sorted keys from a FileInfo map for error messages.
func statusKeys(m map[string]FileInfo) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestIndexer_DiscoverSessions verifies that discoverSessions finds session dirs.
func TestIndexer_DiscoverSessions(t *testing.T) {
	htmlgraphDir := t.TempDir()
	sessionsDir := filepath.Join(htmlgraphDir, "sessions")

	// Create two session dirs, one with events.ndjson, one without.
	sess1 := filepath.Join(sessionsDir, "sess-alpha")
	sess2 := filepath.Join(sessionsDir, "sess-beta")
	_ = os.MkdirAll(sess1, 0o755)
	_ = os.MkdirAll(sess2, 0o755)
	_ = os.WriteFile(filepath.Join(sess1, "events.ndjson"), []byte(""), 0o644)
	// sess2 has no events.ndjson

	snk := sqls.New(&fakeWriter{})
	idxr := New(htmlgraphDir, snk)

	sessions, err := idxr.discoverSessions()
	if err != nil {
		t.Fatalf("discoverSessions: %v", err)
	}
	// Only sess-alpha should be returned (has events.ndjson).
	if len(sessions) != 1 {
		t.Errorf("want 1 session with events.ndjson, got %d: %v", len(sessions), sessions)
	}
	if len(sessions) > 0 && sessions[0] != "sess-alpha" {
		t.Errorf("expected sess-alpha, got %q", sessions[0])
	}
}

// Ensure sql package is referenced so import is valid.
var _ = sql.ErrNoRows
