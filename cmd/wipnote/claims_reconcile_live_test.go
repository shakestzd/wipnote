package main

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const (
	liveRoot  = "019ee378-abcd-7000-8000-0000000004a1"
	endedRoot = "019ee378-abcd-7000-8000-0000000004a2"
)

// TestClaimsReconcile_KeepsLiveForeignSession: reconcile must not close the
// episodes of a session that has not ended in the sessions ledger, and must
// close those of a session that has.
func TestClaimsReconcile_KeepsLiveForeignSession(t *testing.T) {
	_, wipnoteDir := setupArchiveRepo(t)
	start := time.Now().UTC().Add(-2 * time.Hour)
	sessions := sessionledger.NewStore(wipnoteDir)
	for _, sid := range []string{liveRoot, endedRoot} {
		if _, err := sessions.Open(sessionledger.Record{SessionID: sid, Harness: "claude-code", StartedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.Close(endedRoot, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	seedEpisode(t, wipnoteDir, liveRoot, liveRoot, "ag-1", "feat-a", start, time.Time{}, "")
	seedEpisode(t, wipnoteDir, endedRoot, endedRoot, "ag-2", "feat-b", start, time.Time{}, "")

	store := claimledger.NewStore(wipnoteDir)
	res, err := store.Reconcile(claimLedgerLivePredicate(wipnoteDir), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Episodes != 1 || res.Sessions != 1 {
		t.Fatalf("closed %d episode(s) in %d session(s), want 1 and 1", res.Episodes, res.Sessions)
	}
	live, _ := store.ReadShard(liveRoot)
	if len(live) != 1 || !live[0].IsOpen() {
		t.Fatalf("live session's episode was closed: %+v", live)
	}
	ended, _ := store.ReadShard(endedRoot)
	if len(ended) != 1 || ended[0].IsOpen() {
		t.Fatalf("ended session's episode stayed open: %+v", ended)
	}
}
