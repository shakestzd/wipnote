package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// TestWho_LiveFollowsSessionsLedger pins that `wipnote who` judges liveness by
// the sessions ledger, the rule claims reconcile uses: a session with no
// recorded end is live, an ended one is not. The claims-table heartbeat it
// used before is never written for ledger-projected claims, so every session
// read as not live.
func TestWho_LiveFollowsSessionsLedger(t *testing.T) {
	_, wipnoteDir := setupArchiveRepo(t)
	sessions := sessionledger.NewStore(wipnoteDir)
	start := time.Now().UTC().Add(-time.Hour)
	for _, sid := range []string{liveRoot, endedRoot} {
		if _, err := sessions.Open(sessionledger.Record{SessionID: sid, Harness: "claude-code", StartedAt: start}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sessions.Close(endedRoot, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	for sid, want := range map[string]bool{liveRoot: true, endedRoot: false} {
		if got := whoLiveFor(t, sid); got != want {
			t.Errorf("who live for %s = %v, want %v", sid, got, want)
		}
	}
}

// whoLiveFor runs `wipnote who --json` as session sid and returns its live field.
func whoLiveFor(t *testing.T, sid string) bool {
	t.Helper()
	t.Setenv("WIPNOTE_HARNESS", "claude")
	t.Setenv("CLAUDE_CODE_SESSION_ID", sid)
	t.Setenv("CLAUDE_SESSION_ID", "")
	out := captureStdout(t, func() {
		if err := runWho(true); err != nil {
			t.Fatalf("runWho: %v", err)
		}
	})
	var got whoOutput
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("who --json: %v\n%s", err, out)
	}
	if got.SessionID != sid {
		t.Fatalf("who resolved session %q, want %q", got.SessionID, sid)
	}
	return got.Live
}
