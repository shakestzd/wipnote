//go:build integration

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/PuerkitoBio/goquery"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/ingest"
	"github.com/shakestzd/wipnote/core/models"
)

// setupRoundTripEnv prepares a temp project with an on-disk db at the
// expected `.wipnote/wipnote.db` location and returns the paths.
// The db is left closed — callers re-open as needed so sweep and reindex
// both hit a real file that survives between goroutines.
func setupRoundTripEnv(t *testing.T) (projectDir, wipnoteDir, dbPath string) {
	t.Helper()
	projectDir = t.TempDir()
	wipnoteDir = filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(filepath.Join(wipnoteDir, "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	dbPath = filepath.Join(wipnoteDir, "wipnote.db")

	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	database.Close()
	return
}

// TestSessionRoundTrip_IngestThenReindex verifies that an ingested session
// renders HTML, the HTML parses via goquery, and the rebuilt SQLite row
// after a reindex matches the expected event count.
func TestSessionRoundTrip_IngestThenReindex(t *testing.T) {
	_, wipnoteDir, dbPath := setupRoundTripEnv(t)

	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	sessionID := "sess-roundtrip-ingest-001"
	msgTs := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	result := &ingest.ParseResult{
		SessionID: sessionID,
		Messages: []models.Message{
			{Ordinal: 0, Role: "user", Timestamp: msgTs},
			{Ordinal: 1, Role: "assistant", Timestamp: msgTs.Add(2 * time.Second)},
		},
		ToolCalls: []models.ToolCall{
			{MessageOrdinal: 1, ToolName: "Read", ToolUseID: "tu-a", InputJSON: `{"file_path":"/mock/a.go"}`},
			{MessageOrdinal: 1, ToolName: "Edit", ToolUseID: "tu-b", InputJSON: `{"file_path":"/mock/a.go"}`},
			{MessageOrdinal: 1, ToolName: "Bash", ToolUseID: "tu-c", InputJSON: `{"command":"go test"}`},
		},
	}

	sess := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		CreatedAt:     msgTs,
		Status:        "completed",
	}
	if err := dbtest.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	if err := hooks.RenderIngestedSessionHTML(wipnoteDir, sessionID, "/src/project", result, false); err != nil {
		t.Fatalf("render: %v", err)
	}

	// Blow away every agent_events row so reindex starts from a clean slate.
	if _, err := database.Exec(`DELETE FROM agent_events`); err != nil {
		t.Fatalf("delete events: %v", err)
	}

	total, upserted, errFiles := reindexSessions(database, filepath.Join(wipnoteDir, "sessions"), filepath.Dir(wipnoteDir))
	if errFiles != 0 {
		t.Fatalf("reindex errors: %d", errFiles)
	}
	if total == 0 {
		t.Fatal("reindex found no session files")
	}
	if upserted != len(result.ToolCalls) {
		t.Errorf("upserted: got %d, want %d", upserted, len(result.ToolCalls))
	}

	var count int
	if err := database.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE session_id = ?`, sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != len(result.ToolCalls) {
		t.Errorf("agent_events count: got %d, want %d", count, len(result.ToolCalls))
	}
}

// TestSessionRoundTrip_OrphanSweepReindexesAsAborted proves that a swept
// orphan comes back as status=aborted after a reindex.
func TestSessionRoundTrip_OrphanSweepReindexesAsAborted(t *testing.T) {
	projectDir, wipnoteDir, dbPath := setupRoundTripEnv(t)

	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	sessionID := "sess-roundtrip-sweep-001"
	sess := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		CreatedAt:     time.Now().UTC(),
		Status:        "active",
	}
	if err := dbtest.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	hooks.CreateSessionHTML(projectDir, sess)

	seedToolLogOrphan(t, projectDir, sessionID, "evt-sweep-roundtrip-1", time.Now().UTC().Add(-10*time.Minute))
	if hooks.SweepOrphanedToolCalls(projectDir, "", time.Now().UTC(), 0) != 1 {
		t.Fatal("sweep should have appended one synthetic entry")
	}

	// Drop the row and reindex — the rebuilt row must come back as aborted.
	if _, err := database.Exec(`DELETE FROM agent_events`); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	_, upserted, errFiles := reindexSessions(database, filepath.Join(wipnoteDir, "sessions"), filepath.Dir(wipnoteDir))
	if errFiles != 0 {
		t.Fatalf("reindex errors: %d", errFiles)
	}
	if upserted != 1 {
		t.Errorf("upserted: got %d, want 1", upserted)
	}

	evt, err := dbtest.GetEvent(database, "evt-sweep-roundtrip-1")
	if err != nil {
		t.Fatalf("GetEvent: %v", err)
	}
	if evt.Status != "aborted" {
		t.Errorf("reindexed status: got %q, want aborted", evt.Status)
	}
}

// TestSessionRoundTrip_ConcurrentWriters is the load-bearing acceptance
// criterion for the plan. Spawns 20 goroutines across three logical writer
// types — ingest render, live PostToolUse append, and orphan sweep append —
// all targeting the SAME session HTML file, then parses the result via
// goquery and verifies every single event lands in the final file matched
// by data-event-id. Anything less than "all 20 present" is a failure.
func TestSessionRoundTrip_ConcurrentWriters(t *testing.T) {
	projectDir, wipnoteDir, dbPath := setupRoundTripEnv(t)

	database, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer database.Close()

	sessionID := "sess-concurrent-001"
	sess := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		CreatedAt:     time.Now().UTC(),
		Status:        "active",
	}
	if err := dbtest.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	hooks.CreateSessionHTML(projectDir, sess)

	const (
		liveWriters   = 8
		ingestWriters = 4
		sweepWriters  = 8
	)
	total := liveWriters + ingestWriters + sweepWriters

	// Pre-seed one unmatched tool-log start per sweep writer so the sweep
	// path has something to turn into a synthetic aborted entry.
	old := time.Now().UTC().Add(-10 * time.Minute)
	sweepIDs := make([]string, sweepWriters)
	for i := 0; i < sweepWriters; i++ {
		sweepIDs[i] = fmt.Sprintf("evt-sweep-%02d", i)
		seedToolLogOrphan(t, projectDir, sessionID, sweepIDs[i], old)
	}

	// Expected event IDs must include every writer's event id so the
	// final assertion covers all 20 targets.
	expected := make(map[string]struct{}, total)

	var wg sync.WaitGroup
	start := make(chan struct{})

	// Live PostToolUse-style appends.
	for i := 0; i < liveWriters; i++ {
		id := fmt.Sprintf("evt-live-%02d", i)
		expected[id] = struct{}{}
		wg.Add(1)
		go func(eventID string) {
			defer wg.Done()
			<-start
			hooks.AppendEventToSessionHTML(projectDir, sessionID, hooks.SessionEvent{
				Timestamp: time.Now().UTC(),
				ToolName:  "Read",
				Success:   true,
				EventID:   eventID,
				Summary:   "live append",
			})
		}(id)
	}

	// Ingest render-style appends (same mechanism, different label).
	for i := 0; i < ingestWriters; i++ {
		id := fmt.Sprintf("evt-ingest-%02d", i)
		expected[id] = struct{}{}
		wg.Add(1)
		go func(eventID string) {
			defer wg.Done()
			<-start
			hooks.AppendEventToSessionHTML(projectDir, sessionID, hooks.SessionEvent{
				Timestamp: time.Now().UTC(),
				ToolName:  "Edit",
				Success:   true,
				EventID:   eventID,
				Summary:   "ingest render",
			})
		}(id)
	}

	// Sweep-style writers — each goroutine runs the sweep scoped to the
	// project, which calls AppendEventToSessionHTML with data-status="aborted"
	// for every pre-seeded orphan it finds. One goroutine holds the tool log's
	// sweeper lock and appends all N entries; the rest skip — that's OK. The expected set
	// already includes the sweep IDs from the pre-seed loop above.
	for _, id := range sweepIDs {
		expected[id] = struct{}{}
	}
	for i := 0; i < sweepWriters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			hooks.SweepOrphanedToolCalls(projectDir, "", time.Now().UTC(), 0)
		}()
	}

	// Release the herd.
	close(start)
	wg.Wait()

	// Parse via goquery and collect every data-event-id.
	htmlPath := filepath.Join(wipnoteDir, "sessions", sessionID+".html")
	f, err := os.Open(htmlPath)
	if err != nil {
		t.Fatalf("open final html: %v", err)
	}
	defer f.Close()
	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		t.Fatalf("parse final html: %v", err)
	}

	present := map[string]struct{}{}
	doc.Find("li[data-event-id]").Each(func(_ int, s *goquery.Selection) {
		if v, ok := s.Attr("data-event-id"); ok {
			present[v] = struct{}{}
		}
	})

	var missing []string
	for id := range expected {
		if _, ok := present[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		data, _ := os.ReadFile(htmlPath)
		t.Errorf("concurrent writers lost %d of %d events: %s\nfinal html:\n%s",
			len(missing), len(expected), strings.Join(missing, ", "), string(data))
	}

	// Round-trip through reindex and confirm every expected event comes back.
	if _, err := database.Exec(`DELETE FROM agent_events`); err != nil {
		t.Fatalf("delete events: %v", err)
	}
	_, upserted, errFiles := reindexSessions(database, filepath.Join(wipnoteDir, "sessions"), filepath.Dir(wipnoteDir))
	if errFiles != 0 {
		t.Fatalf("reindex errors: %d", errFiles)
	}
	if upserted != len(expected) {
		t.Errorf("reindex upserted: got %d, want %d", upserted, len(expected))
	}
}

// seedToolLogOrphan writes an unmatched PreToolUse start line for id into the
// session's canonical tool log and marks the session's owner process dead, so
// the session-start sweep records it as aborted (feat-174dc958).
func seedToolLogOrphan(t *testing.T, projectDir, sessionID, id string, at time.Time) {
	t.Helper()
	dir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(dir, "tool-events.ndjson"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := fmt.Fprintf(f, "{\"t\":\"Bash\",\"id\":%q,\"ts\":%d}\n", id, at.Unix()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".session-pid"), []byte("2147483647\n1"), 0o644); err != nil {
		t.Fatal(err)
	}
}
