package indexer

import (
	"context"
	"database/sql"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"
	sqls "github.com/shakestzd/wipnote/observe/otel/sink/sqlite"
)

// TestIndexer_PromptIDReindexPath verifies the legacy / reindex fallback
// branch in maybeSetPromptID.
//
// The ACTUAL wipnote reindex wiring (cmd/wipnote/reindex_otel_events.go:84-86)
// is:
//
//	idxr := indexer.New(wipnoteDir, sink)
//	if bridgeDB != nil {
//	    idxr = idxr.WithDB(bridgeDB)   // writable handle, no WithWriteDB
//	}
//
// That exercises the third branch of maybeSetPromptID: idx.database (the
// legacy/single-handle fallback), NOT idx.writeDB.  This
// test constructs the indexer with exactly that wiring so a regression in
// the idx.database branch would be caught here.
func TestIndexer_PromptIDReindexPath(t *testing.T) {
	w, dbPath := setupIndexerDB(t)

	// bridgeDB: writable handle, passed only through WithDB — exactly what
	// reindex_otel_events.go does.  WithWriteDB and WithQueue are NOT called.
	bridgeDB, err := dbtest.Open(dbPath)
	if err != nil {
		t.Fatalf("open bridgeDB: %v", err)
	}
	defer bridgeDB.Close()

	sessionID := "prmq-reindex-sess"
	if _, err := bridgeDB.Exec(
		`INSERT INTO sessions (session_id, agent_assigned) VALUES (?, 'claude-code')
		 ON CONFLICT(session_id) DO NOTHING`,
		sessionID,
	); err != nil {
		t.Fatalf("seed sessions: %v", err)
	}

	eventID := "prmq-reindex-evt"
	if _, err := bridgeDB.Exec(
		`INSERT INTO agent_events
		     (event_id, agent_id, event_type, tool_name, session_id, timestamp, prompt_id)
		 VALUES (?, 'human', 'tool_call', 'UserQuery', ?, '2026-05-18T10:00:00Z', NULL)`,
		eventID, sessionID,
	); err != nil {
		t.Fatalf("seed agent_events: %v", err)
	}

	wipnoteDir := t.TempDir()
	promptID := "prmq-reindex-prompt"
	line := `{"kind":"log","harness":"claude_code","ts":"2026-05-18T10:00:00Z","signal_id":"prmq-rx-s1","session_id":"prmq-reindex-sess","canonical":"user_prompt","native":"claude_code.user_prompt","prompt_id":"prmq-reindex-prompt"}`
	writeNDJSONFixture(t, wipnoteDir, sessionID, []string{line})

	// Mirror reindex_otel_events.go exactly: New + WithDB only.
	// No WithWriteDB, no WithQueue — exercises the idx.database legacy branch.
	idxr := New(wipnoteDir, sqls.New(w)).
		WithDB(bridgeDB)

	if err := idxr.processSession(context.Background(), sessionID); err != nil {
		t.Fatalf("processSession: %v", err)
	}

	var gotPromptID sql.NullString
	if err := bridgeDB.QueryRow(
		`SELECT prompt_id FROM agent_events WHERE event_id = ?`, eventID,
	).Scan(&gotPromptID); err != nil {
		t.Fatalf("query prompt_id: %v", err)
	}
	if !gotPromptID.Valid || gotPromptID.String != promptID {
		t.Errorf("reindex path: prompt_id = %v, want %q", gotPromptID, promptID)
	}
}
