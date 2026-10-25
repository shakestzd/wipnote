package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// TestSessionStart_ClosesOnlySessionsThatReportedAnEnd pins what SessionStart
// does to OTHER sessions (feat-174dc958). A session that crashed (open ledger
// row, dead owner pid, no end note) is left exactly as it was: session start
// no longer reaps stale sessions. A session whose SessionEnd hook was cancelled
// after it wrote its end note is still closed from that note.
func TestSessionStart_ClosesOnlySessionsThatReportedAnEnd(t *testing.T) {
	f := newEndFixture(t, 0)
	clearNestedSessionEnv(t)
	database, err := openWipnoteTestDB(t, f.projectDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	endedAt := time.Now().UTC().Add(-20 * time.Minute)
	f.writeNote(t, endedAt, "abandoned")

	const crashed = "cccc1111-dddd-eeee-ffff-000000004444"
	ledger := sessionledger.NewStore(f.wipnoteDir)
	if _, err := ledger.Open(sessionledger.Record{
		SessionID: crashed, Harness: "claude_code", ProjectDir: ".",
		StartedAt: time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sessions (session_id, agent_assigned, created_at, status)
		VALUES (?, 'claude-code', ?, 'active')`, crashed, time.Now().UTC().Add(-2*time.Hour).Format(time.RFC3339)); err != nil {
		t.Fatal(err)
	}
	crashedDir := filepath.Join(f.wipnoteDir, "sessions", crashed)
	if err := os.MkdirAll(crashedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(crashedDir, ".session-pid"), []byte("2147483647\n1"), 0o644); err != nil {
		t.Fatal(err)
	}

	event := &CloudEvent{SessionID: "sess-new-start-0001", CWD: f.projectDir}
	if _, err := SessionStart(event, database, f.projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	rec, found, err := ledger.Get(crashed)
	if err != nil || !found {
		t.Fatalf("crashed ledger row: found=%v err=%v", found, err)
	}
	if !rec.IsOpen() {
		t.Errorf("crashed session was closed in the ledger at %v; session start must not reap", rec.EndedAt)
	}
	if row := f.ledgerRow(t); row.IsOpen() || row.EndedAt.Sub(endedAt).Abs() > time.Second {
		t.Errorf("ended session ledger end = %v open=%v, want the note's %v", row.EndedAt, row.IsOpen(), endedAt)
	}
}
