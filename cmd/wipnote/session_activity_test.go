package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

const sessionActivityFixtureID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

// writeActivityHTML writes a minimal session HTML file whose activity log
// holds one <li> per timestamp, in the exact shape AppendEventToSessionHTML
// emits (data-ts is the first attribute).
func writeActivityHTML(t *testing.T, wipnoteDir, sessionID string, stamps ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><body><article id="` + sessionID + `" data-status="active">`)
	b.WriteString(`<p>preamble with <li data-ts="1999-01-01T00:00:00Z"> outside the log</p>`)
	b.WriteString(`<section data-activity-log><ol>`)
	for i, ts := range stamps {
		b.WriteString(`<li data-ts="` + ts + `" data-tool="Bash" data-success="true" data-event-id="ev-` + string(rune('a'+i)) + `">call</li>`)
	}
	b.WriteString(`</ol></section></article></body></html>`)
	dir := filepath.Join(wipnoteDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, sessionID+".html"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSessionActivityFromHookLog(t *testing.T) {
	wipnoteDir := t.TempDir()
	writeActivityHTML(t, wipnoteDir, sessionActivityFixtureID,
		"2026-09-21T10:00:00Z", "2026-09-21T10:05:30Z", "2026-09-21T10:02:00Z", "not-a-time")

	act := sessionActivityFor(wipnoteDir, sessionActivityFixtureID)
	if act.Source != "hooks" {
		t.Fatalf("source: got %q want hooks", act.Source)
	}
	// Entries outside <section data-activity-log> are not counted; the
	// unparsable stamp still counts as a call but never becomes LastCall.
	if act.Calls != 4 {
		t.Errorf("calls: got %d want 4", act.Calls)
	}
	want := time.Date(2026, 9, 21, 10, 5, 30, 0, time.UTC)
	if !act.LastCall.Equal(want) {
		t.Errorf("last call: got %v want %v", act.LastCall, want)
	}
}

func TestSessionActivityFallsBackToSignalStream(t *testing.T) {
	wipnoteDir := t.TempDir()
	sessDir := filepath.Join(wipnoteDir, "sessions", sessionActivityFixtureID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ndjson := filepath.Join(sessDir, "events.ndjson")
	if err := os.WriteFile(ndjson, []byte(`{"kind":"x","ts":"2026-09-21T10:00:00Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 9, 21, 9, 30, 0, 0, time.UTC)
	if err := os.Chtimes(ndjson, stamp, stamp); err != nil {
		t.Fatal(err)
	}

	act := sessionActivityFor(wipnoteDir, sessionActivityFixtureID)
	if act.Source != "otel" || act.Calls != 0 {
		t.Fatalf("got %+v, want otel source with no call count", act)
	}
	if !act.LastCall.Equal(stamp) {
		t.Errorf("last call: got %v want %v", act.LastCall, stamp)
	}
	if got := act.callsColumn(); got != "-" {
		t.Errorf("calls column for otel-only data: got %q want -", got)
	}
	if got := act.describeLastCall(stamp.Add(90 * time.Second)); !strings.Contains(got, "1m30s ago") || !strings.Contains(got, "signal stream") {
		t.Errorf("describeLastCall: %q", got)
	}
}

func TestSessionActivityNoData(t *testing.T) {
	act := sessionActivityFor(t.TempDir(), sessionActivityFixtureID)
	if act.Source != "" || act.Calls != 0 || !act.LastCall.IsZero() {
		t.Fatalf("expected empty activity, got %+v", act)
	}
	now := time.Now()
	if act.callsColumn() != "-" || act.lastCallColumn(now) != "-" {
		t.Errorf("columns: %q %q", act.callsColumn(), act.lastCallColumn(now))
	}
	if got := act.describeLastCall(now); got != "- (no tool calls recorded)" {
		t.Errorf("describeLastCall: %q", got)
	}
}

func TestSessionListColumnsShowCallsAndLastCall(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 10, 0, 0, time.UTC)
	s := &models.Session{
		SessionID:     sessionActivityFixtureID,
		AgentAssigned: "claude-code",
		CreatedAt:     now.Add(-time.Hour),
		Status:        "active",
	}
	cases := []struct {
		name     string
		act      sessionActivity
		wantCols []string
	}{
		{
			name:     "hook data present",
			act:      sessionActivity{Calls: 23, LastCall: now.Add(-4*time.Minute - 12*time.Second), Source: "hooks"},
			wantCols: []string{"23", "4m12s ago"},
		},
		{
			name:     "never started",
			act:      sessionActivity{},
			wantCols: []string{"-", "-"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			printSessionListHeader(&buf)
			printSessionRow(&buf, s, tc.act, now)
			out := buf.String()
			lines := strings.Split(strings.TrimSpace(out), "\n")
			if len(lines) != 3 {
				t.Fatalf("want header, rule, row; got:\n%s", out)
			}
			for _, h := range []string{"SESSION", "AGENT", "STATUS", "STARTED", "DURATION", "CALLS", "LAST CALL"} {
				if !strings.Contains(lines[0], h) {
					t.Errorf("header missing %q: %s", h, lines[0])
				}
			}
			fields := strings.Fields(lines[2])
			// SESSION AGENT STATUS DATE TIME DURATION CALLS LAST-CALL...
			if len(fields) < 7 {
				t.Fatalf("row has too few columns: %q", lines[2])
			}
			calls := fields[6]
			last := strings.Join(fields[7:], " ")
			if calls != tc.wantCols[0] || last != tc.wantCols[1] {
				t.Errorf("row columns: calls=%q last=%q want %v\nrow: %s", calls, last, tc.wantCols, lines[2])
			}
		})
	}
}

func TestSessionShowRendersActivity(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 10, 0, 0, time.UTC)
	act := sessionActivity{Calls: 7, LastCall: now.Add(-30 * time.Second), Source: "hooks"}
	var buf bytes.Buffer
	renderSessionActivity(&buf, act, now)
	out := buf.String()
	if !strings.Contains(out, "Calls     7") {
		t.Errorf("missing calls line:\n%s", out)
	}
	if !strings.Contains(out, "Last call ") || !strings.Contains(out, "(0m30s ago)") {
		t.Errorf("missing last-call line:\n%s", out)
	}

	buf.Reset()
	s := &models.Session{SessionID: sessionActivityFixtureID, AgentAssigned: "claude-code", CreatedAt: now, Status: "active"}
	if err := renderSessionShowCanonical(&buf, s, sessionActivity{}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Calls     -") || !strings.Contains(buf.String(), "no tool calls recorded") {
		t.Errorf("session show without data should say so:\n%s", buf.String())
	}
}
