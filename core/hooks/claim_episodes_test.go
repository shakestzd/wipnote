package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
)

// TestCloseClaimEpisodesForSession_OnlyThatSession pins the session-scoped
// expired close a delegate verb runs for a lost worker (plan-28b4a7f6
// slice 16): every open episode of that session closes, in whatever shard
// holds it, and nobody else's.
func TestCloseClaimEpisodesForSession_OnlyThatSession(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := claimledger.NewStore(wipnoteDir)
	open := func(root, sid, item string) {
		t.Helper()
		if _, _, err := store.Open(root, claimledger.Episode{WorkItemID: item, SessionID: sid, RootSessionID: root,
			AgentID: "__root__", StartedAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
			t.Fatal(err)
		}
	}
	open("sid-worker", "sid-worker", "feat-aaaa")
	open("sid-worker", "sid-worker", "feat-bbbb")
	open("sid-worker", "sid-child", "feat-aaaa") // a sibling in the same shard
	open("sid-caller", "sid-caller", "feat-aaaa")

	n, err := CloseClaimEpisodesForSession(wipnoteDir, "sid-worker", claimledger.OutcomeExpired)
	if err != nil || n != 2 {
		t.Fatalf("closed %d err %v, want 2", n, err)
	}
	eps, _ := store.ReadAll()
	for _, e := range eps {
		mine := e.SessionID == "sid-worker"
		if mine && (e.IsOpen() || e.Outcome != claimledger.OutcomeExpired) {
			t.Fatalf("worker episode %+v", e)
		}
		if !mine && !e.IsOpen() {
			t.Fatalf("closed someone else's episode %+v", e)
		}
	}
	// Other side: a session with no episode, or no session, is a no-op.
	for _, sid := range []string{"sid-none", ""} {
		if n, err := CloseClaimEpisodesForSession(wipnoteDir, sid, claimledger.OutcomeExpired); n != 0 || err != nil {
			t.Fatalf("%q: closed %d err %v", sid, n, err)
		}
	}
}
