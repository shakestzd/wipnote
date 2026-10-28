package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// ledgerTestSessionID is session-shaped, which the ledger requires — an id that
// the target-validity gate would not recognise as a session must never become a
// row (that is what would let an arbitrary token pass the gate).
const ledgerTestSessionID = "aaaabbbb-cccc-dddd-eeee-ffff00001111"

func newLedgerHookProject(t *testing.T) string {
	t.Helper()
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")
	t.Setenv("CLAUDE_ENV_FILE", "")
	return projectDir
}

func ledgerRows(t *testing.T, projectDir string) []sessionledger.Record {
	t.Helper()
	recs, err := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote")).ReadAll()
	if err != nil {
		t.Fatalf("read sessions ledger: %v", err)
	}
	return recs
}

// TestSessionStartWritesTheCanonicalRow is the start-time-creation contract.
// The row has to exist from the moment the session begins, not from the moment
// it is archived: an implemented_in edge written when a work item completes must
// never name a target that is not yet durable, and a session that dies without
// ever archiving must still leave a record behind.
func TestSessionStartWritesTheCanonicalRow(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
	if _, err := SessionStart(event, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	recs := ledgerRows(t, projectDir)
	if len(recs) != 1 {
		t.Fatalf("got %d ledger rows after one session start, want 1", len(recs))
	}
	if recs[0].SessionID != ledgerTestSessionID {
		t.Errorf("session id: got %q, want %q", recs[0].SessionID, ledgerTestSessionID)
	}
	if recs[0].StartedAt.IsZero() {
		t.Error("the row has no start time")
	}
	if !recs[0].IsOpen() {
		t.Errorf("a session that has only started must be open, got end %v", recs[0].EndedAt)
	}
	// The ledger is git-tracked; .wipnote/sessions/ is not. A row that landed in
	// the gitignored tree would vanish on a fresh clone, which is the whole
	// failure this feature exists to fix.
	if _, statErr := os.Stat(filepath.Join(projectDir, ".wipnote", sessionledger.FileName)); statErr != nil {
		t.Errorf("ledger is not at .wipnote/%s: %v", sessionledger.FileName, statErr)
	}
}

// TestSessionStartIsIdempotentAcrossResume pins the once-per-session write cost.
// SessionStart fires again on --resume and --continue with the same id.
func TestSessionStartIsIdempotentAcrossResume(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
	for i := 0; i < 3; i++ {
		if _, err := SessionStart(event, projectDir); err != nil {
			t.Fatalf("SessionStart #%d: %v", i+1, err)
		}
	}

	if recs := ledgerRows(t, projectDir); len(recs) != 1 {
		t.Errorf("got %d ledger rows after three starts of the same session, want 1 — "+
			"a resumed session would be counted three times", len(recs))
	}
}

// TestSessionStartSkipsSubagents is the root-sessions-only rule. Subagents get
// synthetic sessions rows keyed by agent id; those ids are lineage bookkeeping,
// are never edge targets, and one row each would bury the real sessions.
func TestSessionStartSkipsSubagents(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	t.Setenv("WIPNOTE_PARENT_SESSION", "11112222-3333-4444-5555-666677778888")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "1")

	event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
	if _, err := SessionStart(event, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	if recs := ledgerRows(t, projectDir); len(recs) != 0 {
		t.Errorf("a subagent session got %d ledger row(s); the ledger holds root sessions only", len(recs))
	}
}

// TestSessionLedgerCloseStampsEnd covers the SessionEnd half without driving the
// whole SessionEnd handler, which does far more than this one write.
func TestSessionLedgerCloseStampsEnd(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	store := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote"))
	if _, err := store.Open(sessionledger.Record{
		SessionID: ledgerTestSessionID,
		Harness:   "claude-code",
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatalf("seed row: %v", err)
	}

	recordSessionLedgerClose(projectDir, ledgerTestSessionID, time.Now().UTC())

	recs := ledgerRows(t, projectDir)
	if len(recs) != 1 {
		t.Fatalf("got %d rows, want 1", len(recs))
	}
	if recs[0].IsOpen() {
		t.Error("the row is still open after the session ended")
	}
}

// TestSessionLedgerCloseOfUnrecordedSessionIsSilent covers the sessions that
// predate the ledger and the subagent sessions that deliberately have no row:
// both reach SessionEnd, and neither is a failure.
func TestSessionLedgerCloseOfUnrecordedSessionIsSilent(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	recordSessionLedgerClose(projectDir, ledgerTestSessionID, time.Now().UTC())
	if recs := ledgerRows(t, projectDir); len(recs) != 0 {
		t.Errorf("closing an unrecorded session created %d row(s); it must not invent history", len(recs))
	}
}

// TestSessionStartResumeOfAClosedSessionAppendsARun is bug-2a96e9cb at the hook:
// a session that ended and is resumed records a second run, so its second end
// is recorded instead of the row staying closed at the first.
func TestSessionStartResumeOfAClosedSessionAppendsARun(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	if _, err := SessionStart(&CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir, Source: "startup"}, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	firstEnd := time.Now().UTC()
	recordSessionLedgerClose(projectDir, ledgerTestSessionID, firstEnd)

	if _, err := SessionStart(&CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir, Source: "resume"}, projectDir); err != nil {
		t.Fatalf("SessionStart resume: %v", err)
	}
	recs := ledgerRows(t, projectDir)
	if len(recs) != 1 || !recs[0].IsOpen() || len(recs[0].RunList()) != 2 {
		t.Fatalf("after resume: %+v, want one open row with two runs", recs)
	}

	secondEnd := time.Now().UTC().Add(time.Second)
	recordSessionLedgerClose(projectDir, ledgerTestSessionID, secondEnd)
	rec := ledgerRows(t, projectDir)[0]
	if !rec.EndedAt.Equal(secondEnd) {
		t.Errorf("end = %v, want the resumed run's end %v", rec.EndedAt, secondEnd)
	}
	if runs := rec.RunList(); !runs[0].EndedAt.Equal(firstEnd) {
		t.Errorf("first run end = %v, want %v", runs[0].EndedAt, firstEnd)
	}
}

// TestLateWriterNeverReopensAClosedSession: a CLI command run by a process
// that outlived its session must not record a resume.
func TestLateWriterNeverReopensAClosedSession(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	store := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote"))
	if _, err := store.Open(sessionledger.Record{SessionID: ledgerTestSessionID, Harness: "claude",
		StartedAt: time.Now().UTC().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	recordSessionLedgerClose(projectDir, ledgerTestSessionID, time.Now().UTC())

	t.Setenv("WIPNOTE_SESSION_ID", ledgerTestSessionID)
	t.Setenv("CLAUDECODE", "1")
	if _, err := agent.EnsureSession(nil, projectDir); err != nil {
		t.Fatalf("EnsureSession: %v", err)
	}
	if rec := ledgerRows(t, projectDir)[0]; rec.IsOpen() || len(rec.RunList()) != 1 {
		t.Errorf("a late writer reopened the session: %+v", rec)
	}
}
