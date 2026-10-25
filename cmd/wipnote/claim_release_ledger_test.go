package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// TestClaimRelease_PersistsToLedger: `claim release` must close the episode in
// the canonical claim ledger. openDB hands back a throwaway projection, so a
// release applied only to that database is lost when the command exits.
func TestClaimRelease_PersistsToLedger(t *testing.T) {
	projectRoot := t.TempDir()
	wipnoteDir := filepath.Join(projectRoot, ".wipnote")
	for _, sub := range []string{"features", "sessions", "claims"} {
		if err := os.MkdirAll(filepath.Join(wipnoteDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", projectRoot)
	const sessionID = "019ee378-abcd-7000-8000-0000000000cc"
	const workItemID = "feat-ab12cd36"
	html := `<!DOCTYPE html><html><body><article id="` + workItemID +
		`" data-type="feature" data-status="in-progress" data-priority="medium">` +
		`<h1>Claimed</h1></article></body></html>`
	if err := os.WriteFile(filepath.Join(wipnoteDir, "features", workItemID+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionledger.NewStore(wipnoteDir).Open(sessionledger.Record{
		SessionID: sessionID, Harness: "claude-code", StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	seedEpisode(t, wipnoteDir, sessionID, sessionID, dbpkg.AgentRootSentinel, workItemID,
		time.Now().UTC().Add(-30*time.Minute), time.Time{}, "")
	eps, err := claimledger.NewStore(wipnoteDir).ReadShard(sessionID)
	if err != nil || len(eps) != 1 {
		t.Fatalf("seed: %v %v", eps, err)
	}

	if err := runClaimRelease(dbpkg.LedgerClaimIDPrefix + eps[0].ID); err != nil {
		t.Fatalf("runClaimRelease: %v", err)
	}
	after, _ := claimledger.NewStore(wipnoteDir).ReadShard(sessionID)
	if len(after) != 1 || after[0].IsOpen() || after[0].Outcome != claimledger.OutcomeReleased {
		t.Fatalf("episode not closed as released in ledger: %+v", after)
	}
}
