package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const resumedRoot = "019ee378-abcd-7000-8000-0000000004a3"

// claimLiveTestDir returns a temp .wipnote dir with the project env unset, so
// nothing here can resolve to a real project.
func claimLiveTestDir(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	dir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// openSessions opens each session in the sessions ledger at start.
func openSessions(t *testing.T, wipnoteDir string, start time.Time, ids ...string) *sessionledger.Store {
	t.Helper()
	sessions := sessionledger.NewStore(wipnoteDir)
	for _, sid := range ids {
		if _, err := sessions.Open(sessionledger.Record{SessionID: sid, Harness: "claude-code", StartedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	return sessions
}

// TestClaimsArchive_KeepsLiveForeignSession (bug-01f182e5): archive must not
// compact the shard of a session still open in the sessions ledger, even when
// all its episodes are closed and old, and must compact an ended one's.
func TestClaimsArchive_KeepsLiveForeignSession(t *testing.T) {
	wipnoteDir := claimLiveTestDir(t)
	start := time.Now().UTC().Add(-60 * 24 * time.Hour)
	sessions := openSessions(t, wipnoteDir, start, liveRoot, endedRoot)
	if err := sessions.Close(endedRoot, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	end := start.Add(time.Hour)
	seedEpisode(t, wipnoteDir, liveRoot, liveRoot, "ag-1", "feat-a", start, end, claimledger.OutcomeCompleted)
	seedEpisode(t, wipnoteDir, endedRoot, endedRoot, "ag-2", "feat-b", start, end, claimledger.OutcomeCompleted)

	cutoff := time.Now().UTC().Add(-30 * 24 * time.Hour)
	res, err := claimledger.NewStore(wipnoteDir).Archive(cutoff, claimLedgerLivePredicate(wipnoteDir), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Candidates) != 1 || res.Candidates[0].RootSessionID != endedRoot {
		got := make([]string, 0, len(res.Candidates))
		for _, c := range res.Candidates {
			got = append(got, c.RootSessionID)
		}
		t.Fatalf("archive candidates = %v, want only the ended session %s", got, endedRoot)
	}
}

// TestClaimsReconcile_KeepsResumedSession (bug-01f182e5): a session that ended
// and was resumed is live again — its latest run is open — so reconcile must
// leave its open episode alone.
func TestClaimsReconcile_KeepsResumedSession(t *testing.T) {
	wipnoteDir := claimLiveTestDir(t)
	start := time.Now().UTC().Add(-2 * time.Hour)
	sessions := openSessions(t, wipnoteDir, start, resumedRoot)
	if err := sessions.Close(resumedRoot, start.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	openSessions(t, wipnoteDir, start.Add(time.Hour), resumedRoot)
	seedEpisode(t, wipnoteDir, resumedRoot, resumedRoot, "ag-1", "feat-a", start.Add(time.Hour), time.Time{}, "")

	store := claimledger.NewStore(wipnoteDir)
	if _, err := store.Reconcile(claimLedgerLivePredicate(wipnoteDir), time.Time{}); err != nil {
		t.Fatal(err)
	}
	eps, _ := store.ReadShard(resumedRoot)
	if len(eps) != 1 || !eps[0].IsOpen() {
		t.Fatalf("resumed session's episode was closed: %+v", eps)
	}
}
