package sessionledger

import (
	"os"
	"testing"
	"time"
)

func TestRepairedHarness(t *testing.T) {
	cases := []struct{ recorded, inferred, want string }{
		{"claude-code", "", "claude"},
		{"claude-code", "codex", "claude"},
		{"human", "codex", "codex"},
		{"human", "", ""},
		{"human", "human", ""},
		{"bogus", "antigravity", "antigravity"},
		{"", "claude", "claude"},
		{"gemini", "claude", "gemini"},
		{"claude", "codex", "claude"},
	}
	for _, c := range cases {
		if got := RepairedHarness(c.recorded, c.inferred); got != c.want {
			t.Errorf("RepairedHarness(%q, %q) = %q, want %q", c.recorded, c.inferred, got, c.want)
		}
	}
}

func TestRetagHarnessDryRunThenApply(t *testing.T) {
	s := testStore(t)
	start := time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)
	if _, err := s.Open(Record{SessionID: sidA, Harness: "claude-code", ProjectDir: ".", StartedAt: start}); err != nil {
		t.Fatalf("open: %v", err)
	}
	before, _ := os.ReadFile(s.Path())

	r, err := s.RetagHarness(sidA, "", true)
	if err != nil || !r.WouldApply || r.Applied || r.Old != "claude-code" || r.New != "claude" {
		t.Fatalf("dry-run = %+v err %v", r, err)
	}
	if after, _ := os.ReadFile(s.Path()); string(after) != string(before) {
		t.Fatal("dry-run wrote to the ledger")
	}

	r, err = s.RetagHarness(sidA, "", false)
	if err != nil || !r.Applied {
		t.Fatalf("apply = %+v err %v", r, err)
	}
	if rec, _, _ := s.Get(sidA); rec.Harness != "claude" {
		t.Fatalf("harness after apply = %q, want claude", rec.Harness)
	}
	if _, err := s.RetagHarness(sidB, "", false); err != ErrNoRow {
		t.Fatalf("missing row: err %v, want ErrNoRow", err)
	}
}
