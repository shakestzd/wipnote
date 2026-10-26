package sessionledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestOpenRefusesTheAgentFallbackAsHarness: "human" is the agent identity
// core/agent falls back to when no harness marks the process. It is nobody's
// harness, so a row must never carry it (bug-5333ea49).
func TestOpenRefusesTheAgentFallbackAsHarness(t *testing.T) {
	s := testStore(t)
	start := time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)
	written, err := s.Open(Record{SessionID: sidA, Harness: "human", ProjectDir: ".", StartedAt: start})
	if !errors.Is(err, ErrAgentHarness) || written {
		t.Fatalf("Open(harness=human) = written %v err %v, want refused with ErrAgentHarness", written, err)
	}
	if _, ok, _ := s.Get(sidA); ok {
		t.Fatal("a row tagged with the agent fallback was written")
	}
	if written, err := s.Open(Record{SessionID: sidB, Harness: "claude", ProjectDir: ".", StartedAt: start}); err != nil || !written {
		t.Fatalf("Open(harness=claude) = written %v err %v, want a row", written, err)
	}
}

// TestEnrichNeverRecordsTheAgentFallback: a late (backfill) row whose only
// harness hint is the agent fallback records the harness as unknown.
func TestEnrichNeverRecordsTheAgentFallback(t *testing.T) {
	s := testStore(t)
	end := time.Date(2026, 10, 26, 6, 0, 0, 0, time.UTC)
	if _, err := s.Enrich(sidA, Enrichment{Harness: "human", EndedAt: end}); err != nil {
		t.Fatalf("Enrich create: %v", err)
	}
	// A later pass with a real harness fills the unknown tag in.
	if _, err := s.Enrich(sidA, Enrichment{Harness: "claude"}); err != nil {
		t.Fatalf("Enrich fill: %v", err)
	}
	rec, ok, err := s.Get(sidA)
	if err != nil || !ok {
		t.Fatalf("Get: ok %v err %v", ok, err)
	}
	if rec.Harness != "claude" {
		t.Errorf("harness = %q, want claude (human must have been recorded as unknown)", rec.Harness)
	}
}

func TestHarnessFromAgent(t *testing.T) {
	cases := map[string]string{
		"claude-code": "claude", "claude": "claude", "codex": "codex",
		"antigravity": "antigravity", "human": "", "": "", "researcher": "",
	}
	for in, want := range cases {
		if got := HarnessFromAgent(in); got != want {
			t.Errorf("HarnessFromAgent(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestOpenCorrectsAGuessedHarness is the race order: a late writer recorded the
// row first with a non-harness value; the authoritative start corrects it, and
// a row that already names a real harness is never retagged.
func TestOpenCorrectsAGuessedHarness(t *testing.T) {
	start := time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)
	cases := []struct{ first, second, want string }{
		{"", "claude", "claude"},
		{"claude-code", "claude", "claude"},
		{"codex", "claude", "codex"},
		{"human", "claude", "claude"}, // a row an older late writer left behind
		{"", "", ""},
	}
	for _, c := range cases {
		s := testStore(t)
		if err := os.MkdirAll(filepath.Dir(s.Path()), 0o755); err != nil {
			t.Fatal(err)
		}
		// Seed below the Open guard, the way an older binary wrote the row.
		if err := appendRowLocked(s.Path(), Record{SessionID: sidA, Harness: c.first, StartedAt: start}); err != nil {
			t.Fatalf("seed row: %v", err)
		}
		written, err := s.Open(Record{SessionID: sidA, Harness: c.second, StartedAt: start.Add(time.Minute)})
		if err != nil || written {
			t.Fatalf("second Open = written %v err %v, want no new row", written, err)
		}
		recs, _ := s.ReadAll()
		if len(recs) != 1 || recs[0].Harness != c.want || !recs[0].StartedAt.Equal(start) {
			t.Errorf("%q then %q: rows %+v, want one row harness %q with the first start", c.first, c.second, recs, c.want)
		}
	}
}
