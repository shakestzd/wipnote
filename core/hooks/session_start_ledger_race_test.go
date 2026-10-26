package hooks

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// raceFirstWriters are the late writers that can reach a session's ledger row
// before its SessionStart hook does (bug-5333ea49): any wipnote CLI command's
// pre-run EnsureSession, with or without a harness marker, and a row an older
// binary or a backfill left with no harness.
var raceFirstWriters = map[string]func(t *testing.T, projectDir string){
	"cli without harness marker": func(t *testing.T, projectDir string) {
		t.Setenv("WIPNOTE_SESSION_ID", ledgerTestSessionID)
		if _, err := agent.EnsureSession(nil, projectDir); err != nil {
			t.Fatalf("EnsureSession: %v", err)
		}
	},
	"cli inside claude": func(t *testing.T, projectDir string) {
		t.Setenv("WIPNOTE_SESSION_ID", ledgerTestSessionID)
		t.Setenv("CLAUDECODE", "1")
		if _, err := agent.EnsureSession(nil, projectDir); err != nil {
			t.Fatalf("EnsureSession: %v", err)
		}
	},
	"row with unknown harness": func(t *testing.T, projectDir string) {
		store := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote"))
		e := sessionledger.Enrichment{Harness: "human", StartedAt: time.Now().UTC().Add(-time.Minute)}
		if _, err := store.Enrich(ledgerTestSessionID, e); err != nil {
			t.Fatalf("Enrich: %v", err)
		}
	},
}

// TestSessionStartAfterALateWriterEndsWithAClaudeRow is the race order: a late
// writer touches the session's row first, the SessionStart hook second. The
// hook's write is authoritative, so the session ends with exactly one row
// tagged claude — never human, never empty.
func TestSessionStartAfterALateWriterEndsWithAClaudeRow(t *testing.T) {
	for name, first := range raceFirstWriters {
		t.Run(name, func(t *testing.T) {
			projectDir := newLedgerHookProject(t)
			t.Setenv("WIPNOTE_SESSION_ID", "")
			t.Setenv("CLAUDECODE", "")
			t.Setenv("CLAUDE_CODE_ENTRYPOINT", "")
			first(t, projectDir)

			t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
			event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
			if _, err := SessionStart(event, projectDir); err != nil {
				t.Fatalf("SessionStart: %v", err)
			}
			recs := ledgerRows(t, projectDir)
			if len(recs) != 1 || recs[0].Harness != "claude" {
				t.Fatalf("ledger rows = %+v, want one row tagged claude", recs)
			}
		})
	}
}
