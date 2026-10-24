package finalize

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func apiRequestLine(sessionID, signalID string, cost string) string {
	return `{"kind":"log","harness":"claude_code","ts":"2026-04-24T19:00:00Z","signal_id":"` + signalID +
		`","session_id":"` + sessionID + `","canonical":"api_request","native":"claude_code.api_request","cost_usd":` + cost + `}`
}

// seedSession writes a session HTML shell and an events.ndjson with two
// api_request logs (total cost 0.75), and returns the NDJSON path.
func seedSession(t *testing.T, projectDir, sessionID string) string {
	t.Helper()
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	sessDir := filepath.Join(sessionsDir, sessionID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<html><body><article id="` + sessionID + `"><h1>s</h1></article></body></html>`
	if err := os.WriteFile(filepath.Join(sessionsDir, sessionID+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	ndjson := apiRequestLine(sessionID, sessionID+"-a", "0.25") + "\n" +
		apiRequestLine(sessionID, sessionID+"-b", "0.5") + "\n"
	path := filepath.Join(sessDir, "events.ndjson")
	if err := os.WriteFile(path, []byte(ndjson), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func readMarker(t *testing.T, ndjsonPath string) (int64, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(filepath.Dir(ndjsonPath), ".index-offset"))
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		t.Fatalf("marker %q: %v", data, err)
	}
	return n, true
}

func readHTML(t *testing.T, projectDir, sessionID string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Session ingests the collector's NDJSON, so the rollup carries the
// api_request cost, and only then marks the whole log consumed.
func TestSession_IngestsMaterializesThenMarks(t *testing.T) {
	projectDir := t.TempDir()
	path := seedSession(t, projectDir, "fin-sess")

	res, err := Session(context.Background(), projectDir, "fin-sess")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if !res.Complete {
		t.Fatalf("result = %+v, want complete", res)
	}
	if html := readHTML(t, projectDir, "fin-sess"); !strings.Contains(html, `data-total-cost-usd="0.750000"`) {
		t.Fatalf("rollup missing collector cost:\n%s", html)
	}
	info, _ := os.Stat(path)
	if got, ok := readMarker(t, path); !ok || got != info.Size() {
		t.Fatalf("marker = %d (present=%v), want %d", got, ok, info.Size())
	}
}

// With the budget spent before the replay ends, nothing was consumed: no
// partial rollup and no marker, so catch-up finishes the job later.
func TestSession_SpentBudgetWritesNothing(t *testing.T) {
	projectDir := t.TempDir()
	path := seedSession(t, projectDir, "fin-sess")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := Session(ctx, projectDir, "fin-sess")
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if res.Complete {
		t.Fatalf("result = %+v, want incomplete", res)
	}
	if _, ok := readMarker(t, path); ok {
		t.Fatal("marker published for an incomplete replay")
	}
	if html := readHTML(t, projectDir, "fin-sess"); strings.Contains(html, "data-has-otel") {
		t.Fatalf("partial rollup written:\n%s", html)
	}
}

// A session without telemetry has nothing to consume and gets no marker.
func TestSession_NoNDJSONIsNoop(t *testing.T) {
	projectDir := t.TempDir()
	res, err := Session(context.Background(), projectDir, "no-telemetry")
	if err != nil || res.Complete {
		t.Fatalf("Session = %+v, %v; want incomplete no-op", res, err)
	}
}

func ageFile(t *testing.T, path string, age time.Duration) {
	t.Helper()
	old := time.Now().Add(-age)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
}

// CatchUp finalizes sessions that never got a clean SessionEnd (crash,
// closed pane, a harness killing the hook) so retention can archive them.
// It skips the active session, logs still being written, and sessions that
// are already marked.
func TestCatchUp_FinalizesOnlyStaleUnmarkedSessions(t *testing.T) {
	projectDir := t.TempDir()
	orphan := seedSession(t, projectDir, "orphan")
	ageFile(t, orphan, time.Hour)
	active := seedSession(t, projectDir, "active")
	ageFile(t, active, time.Hour)
	recent := seedSession(t, projectDir, "recent")
	done := seedSession(t, projectDir, "done")
	ageFile(t, done, time.Hour)
	if _, err := Session(context.Background(), projectDir, "done"); err != nil {
		t.Fatal(err)
	}

	res := CatchUp(context.Background(), projectDir, "active")
	if res.Finalized != 1 {
		t.Fatalf("CatchUp = %+v, want exactly 1 finalized", res)
	}
	if _, ok := readMarker(t, orphan); !ok {
		t.Fatal("orphaned session not marked")
	}
	if !strings.Contains(readHTML(t, projectDir, "orphan"), "data-has-otel") {
		t.Fatal("orphaned session rollup not written")
	}
	for _, p := range []string{active, recent} {
		if _, ok := readMarker(t, p); ok {
			t.Fatalf("CatchUp marked %s, which must be left alone", p)
		}
	}
}
