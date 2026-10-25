package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

func TestRecordSessionEndNote_WritesOneValidLine(t *testing.T) {
	f := newEndFixture(t, 0)
	evt := &CloudEvent{SessionID: f.sessionID, Reason: "logout", TranscriptPath: "/x/t.jsonl"}
	if !RecordSessionEndNote(evt, f.projectDir) {
		t.Fatal("note not written")
	}
	RecordSessionEndNote(evt, f.projectDir) // second call in one process is a no-op
	data, err := os.ReadFile(f.notePath())
	if err != nil {
		t.Fatal(err)
	}
	if n := noteLineCount(data); n != 1 {
		t.Fatalf("note file has %d lines, want 1", n)
	}
	note, ok := ReadSessionEndNote(f.projectDir, f.sessionID)
	if !ok || note.SessionID != f.sessionID || note.Reason != "logout" || note.TranscriptPath != "/x/t.jsonl" {
		t.Fatalf("note = %+v ok=%v", note, ok)
	}
	if time.Since(note.EndTime()) > time.Minute || note.EndTime().IsZero() {
		t.Errorf("ended_at %q not a recent timestamp", note.EndedAt)
	}
}

func noteLineCount(b []byte) int {
	n := 0
	for _, c := range b {
		if c == '\n' {
			n++
		}
	}
	return n
}

func TestRecordSessionEndNote_RefusesPathEscapingIDs(t *testing.T) {
	f := newEndFixture(t, 0)
	for _, id := range []string{"../evil", "a/b", "..", "."} {
		t.Setenv("WIPNOTE_SESSION_ID", id)
		if RecordSessionEndNote(&CloudEvent{SessionID: id}, f.projectDir) {
			t.Errorf("wrote a note for id %q", id)
		}
	}
	if _, err := os.Stat(filepath.Join(f.wipnoteDir, "evil")); err == nil {
		t.Error("an id escaped the sessions directory")
	}
}

// A torn tail (power loss mid-append) must read as "no note", not as garbage;
// a complete line before it still counts.
func TestReadSessionEndNote_IgnoresTornTail(t *testing.T) {
	f := newEndFixture(t, 0)
	if _, ok := ReadSessionEndNote(f.projectDir, f.sessionID); ok {
		t.Fatal("note found before any write")
	}
	f.writeNote(t, time.Now().Add(-time.Minute), "abandoned")
	fh, _ := os.OpenFile(f.notePath(), os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = fh.WriteString(`{"session_id":"` + f.sessionID + `","ended_at":"2026-0`)
	_ = fh.Close()
	if _, ok := ReadSessionEndNote(f.projectDir, f.sessionID); !ok {
		t.Fatal("complete line lost behind a torn tail")
	}
	if err := os.WriteFile(f.notePath(), []byte(`{"session_id":"`+f.sessionID+`","ended_a`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadSessionEndNote(f.projectDir, f.sessionID); ok {
		t.Fatal("a torn-only file must read as no note")
	}
}

// Catch-up closes the ledger row and the episodes from the note, with the
// note's end time and outcome, and a second pass changes nothing.
func TestCatchUp_ClosesOpenStateFromNote_Idempotent(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 2)
	endedAt := time.Now().UTC().Add(-10 * time.Minute)
	f.writeNote(t, endedAt, "abandoned")

	if got := CatchUpEndedSessions(td.DB, f.projectDir, "", false); len(got) != 1 || got[0] != f.sessionID {
		t.Fatalf("first pass finished %v, want [%s]", got, f.sessionID)
	}
	row := f.ledgerRow(t)
	if row.IsOpen() || row.EndedAt.Sub(endedAt).Abs() > time.Second {
		t.Errorf("ledger ended_at = %v (open=%v), want the note's %v", row.EndedAt, row.IsOpen(), endedAt)
	}
	for _, e := range f.episodes(t, f.sessionID) {
		if e.IsOpen() || e.Outcome != claimledger.OutcomeAbandoned || e.EndedAt.Sub(endedAt).Abs() > time.Millisecond {
			t.Errorf("episode %s = open:%v outcome:%q ended:%v; want abandoned at the note's time", e.WorkItemID, e.IsOpen(), e.Outcome, e.EndedAt)
		}
	}
	// Other sessions' episodes are untouched.
	for _, e := range f.episodes(t, "bbbb0000-cccc-dddd-eeee-ffff00003333") {
		if !e.IsOpen() {
			t.Errorf("catch-up closed another session's episode %s", e.WorkItemID)
		}
	}

	shard := claimledger.NewStore(f.wipnoteDir).ShardPath(f.sessionID)
	before, _ := os.ReadFile(shard)
	if got := CatchUpEndedSessions(td.DB, f.projectDir, "", false); len(got) != 0 {
		t.Errorf("second pass finished %v, want nothing (idempotent)", got)
	}
	if after, _ := os.ReadFile(shard); string(after) != string(before) {
		t.Error("second pass rewrote the shard")
	}
}

// A session that ended, resumed and ended again carries a newer note; the old
// done marker must not hide it, and the ledger keeps its first recorded end.
func TestCatchUp_NewerNoteSupersedesMarker(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	f.writeNote(t, time.Now().UTC().Add(-time.Hour), "abandoned")
	CatchUpEndedSessions(td.DB, f.projectDir, "", false)
	f.openEpisode(t, f.sessionID, f.sessionID, "feat-aaaa0003") // claimed after resuming
	f.writeNote(t, time.Now().UTC().Add(-time.Minute), "abandoned")
	if got := CatchUpEndedSessions(td.DB, f.projectDir, "", false); len(got) != 1 {
		t.Fatalf("second end not caught up: %v", got)
	}
	for _, e := range f.episodes(t, f.sessionID) {
		if e.IsOpen() {
			t.Errorf("episode %s left open after the second end", e.WorkItemID)
		}
	}
}

func TestCatchUp_SkipsCurrentSession(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	f.writeNote(t, time.Now().UTC().Add(-time.Minute), "abandoned")
	if got := CatchUpEndedSessions(td.DB, f.projectDir, f.sessionID, true); len(got) != 0 {
		t.Fatalf("caught up the current session: %v", got)
	}
	if !f.ledgerRow(t).IsOpen() {
		t.Error("current session's ledger row was closed")
	}
}

// The cheap root lookup cannot name a root outside {self, parent, family}. The
// session-start pass leaves that episode alone; the deep pass finds it with the
// ledger scan and still uses the note's outcome.
func TestCatchUp_DeepPassFindsUnnamedRootShard(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	const root = "cccc0000-1111-2222-3333-444455556666"
	f.openEpisode(t, root, f.sessionID, "feat-cccc0001")
	// Only the unnamed root holds this session's episodes: no cheap candidate
	// shard exists.
	if err := os.Remove(claimledger.NewStore(f.wipnoteDir).ShardPath(f.sessionID)); err != nil {
		t.Fatal(err)
	}
	f.writeNote(t, time.Now().UTC().Add(-time.Minute), "abandoned")

	CatchUpEndedSessions(td.DB, f.projectDir, "", false)
	if eps := f.episodes(t, root); len(eps) != 1 || !eps[0].IsOpen() {
		t.Fatalf("shallow pass should leave the unnamed-root episode: %+v", eps)
	}
	CatchUpEndedSessions(td.DB, f.projectDir, "", true)
	if eps := f.episodes(t, root); len(eps) != 1 || eps[0].IsOpen() || eps[0].Outcome != claimledger.OutcomeAbandoned {
		t.Fatalf("deep pass should close it as abandoned: %+v", eps)
	}
}

// The session-start reaper runs catch-up first, so a cancelled hook's session
// closes as ABANDONED at its real end, not as EXPIRED at reap time.
func TestReap_ClosesCancelledHookSessionFromNote(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	endedAt := time.Now().UTC().Add(-20 * time.Minute)
	f.writeNote(t, endedAt, "abandoned")

	ReapStaleSessionsAndCollectors(td.DB, f.projectDir, "someone-else", false, false, 5)
	for _, e := range f.episodes(t, f.sessionID) {
		if e.IsOpen() || e.Outcome != claimledger.OutcomeAbandoned {
			t.Errorf("episode %s open=%v outcome=%q, want abandoned", e.WorkItemID, e.IsOpen(), e.Outcome)
		}
	}
	if row := f.ledgerRow(t); row.IsOpen() || row.EndedAt.Sub(endedAt).Abs() > time.Second {
		t.Errorf("ledger end = %v, want the note's %v", row.EndedAt, endedAt)
	}
}

// Happy path: SessionEnd closes the ledger and episodes in the window; the
// handoff, features and lineage follow once catch-up runs, exactly once.
func TestSessionEnd_HappyPath_HandoffWrittenOnceAfterCatchUp(t *testing.T) {
	td := setupTestDB(t)
	f := newEndFixture(t, 0)
	if err := db.UpsertSession(td.DB, &models.Session{
		SessionID: f.sessionID, AgentAssigned: "claude-code", Status: "active",
		CreatedAt: time.Now().UTC().Add(-time.Hour), ProjectDir: f.projectDir,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := td.DB.Exec(`UPDATE sessions SET last_user_query = 'fix the hook' WHERE session_id = ?`, f.sessionID); err != nil {
		t.Fatal(err)
	}

	if _, err := SessionEnd(&CloudEvent{SessionID: f.sessionID, CWD: f.projectDir, Reason: "prompt_input_exit"}, td.DB, f.projectDir); err != nil {
		t.Fatal(err)
	}
	if f.ledgerRow(t).IsOpen() {
		t.Error("ledger open after SessionEnd")
	}
	for _, e := range f.episodes(t, f.sessionID) {
		if e.IsOpen() {
			t.Errorf("episode %s open after SessionEnd", e.WorkItemID)
		}
	}
	sess, _ := db.GetSession(td.DB, f.sessionID)
	if sess == nil || sess.Status != "completed" || sess.HandoffNotes != "" {
		t.Fatalf("before catch-up: status=%v handoff=%q, want completed and no handoff yet", sess, sess.HandoffNotes)
	}

	if got := CatchUpEndedSessions(td.DB, f.projectDir, "", true); len(got) != 1 {
		t.Fatalf("catch-up finished %v", got)
	}
	sess, _ = db.GetSession(td.DB, f.sessionID)
	if sess.HandoffNotes == "" || sess.RecommendedNext == "" {
		t.Fatalf("handoff missing after catch-up: %+v", sess)
	}
	first := sess.HandoffNotes
	if got := CatchUpEndedSessions(td.DB, f.projectDir, "", true); len(got) != 0 {
		t.Errorf("handoff written again: catch-up finished %v", got)
	}
	if sess, _ = db.GetSession(td.DB, f.sessionID); sess.HandoffNotes != first {
		t.Error("handoff changed on a repeated pass")
	}
}
