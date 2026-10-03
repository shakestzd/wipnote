package db_test

// Tests for ProjectClaimsFromEpisodes (bug-ec1ff126).

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
)

// seedOpenEpisode builds a claim_episodes row for the root agent; ended sets
// EndedAt so the episode is closed.
func seedOpenEpisode(t *testing.T, episodeID, workItemID, sessionID string, ended bool) db.ClaimEpisode {
	t.Helper()
	e := db.ClaimEpisode{
		EpisodeID:     episodeID,
		WorkItemID:    workItemID,
		SessionID:     sessionID,
		RootSessionID: sessionID,
		AgentID:       db.AgentRootSentinel,
		StartedAt:     time.Now().UTC().Add(-time.Hour),
	}
	if ended {
		e.EndedAt = time.Now().UTC()
	}
	return e
}

func TestProjectClaimsFromEpisodes_ProjectsOpenEpisodesWithoutHeartbeat(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()

	insertExtraFeature(t, database, "feat-ledger")
	insertExtraSession(t, database, "sess-holder", "claude-code")
	for _, e := range []db.ClaimEpisode{
		seedOpenEpisode(t, "ep-open", "feat-ledger", "sess-holder", false),
		seedOpenEpisode(t, "ep-closed", "feat-ledger", "sess-holder", true),
		// No sessions row: the FK cannot be satisfied, so it must be skipped
		// without failing the pass.
		seedOpenEpisode(t, "ep-orphan", "feat-ledger", "sess-missing", false),
	} {
		if err := db.UpsertClaimEpisode(database, e); err != nil {
			t.Fatalf("UpsertClaimEpisode %s: %v", e.EpisodeID, err)
		}
	}

	n, err := db.ProjectClaimsFromEpisodes(database)
	if err != nil {
		t.Fatalf("ProjectClaimsFromEpisodes: %v", err)
	}
	if n != 1 {
		t.Fatalf("projected %d claims, want 1 (open episode with a session row only)", n)
	}
	// Re-running replaces rather than duplicates.
	if _, err := db.ProjectClaimsFromEpisodes(database); err != nil {
		t.Fatalf("second ProjectClaimsFromEpisodes: %v", err)
	}
	claims, err := db.ListClaimsForWorkItem(database, "feat-ledger")
	if err != nil {
		t.Fatalf("ListClaimsForWorkItem: %v", err)
	}
	if len(claims) != 1 || !db.IsLedgerClaim(claims[0]) || claims[0].OwnerSessionID != "sess-holder" {
		t.Fatalf("claims = %+v, want one ledger claim owned by sess-holder", claims)
	}

	// Heartbeat readers (reaper, reconcile) must keep seeing no heartbeat.
	if db.SessionLivenessByHeartbeat(database, "sess-holder", time.Hour) {
		t.Error("SessionLivenessByHeartbeat = true for a ledger claim; projected rows must not fake a heartbeat")
	}

	// Collision warnings see the projected claimant...
	coll, err := db.DetectCollaboration(database, "feat-ledger")
	if err != nil {
		t.Fatalf("DetectCollaboration: %v", err)
	}
	if len(coll.Claimants) != 1 {
		t.Errorf("DetectCollaboration claimants = %d, want 1", len(coll.Claimants))
	}
	// ...but heartbeat-based LiveCollision does not count it, so `wipnote
	// continue` keeps resuming a session that crashed without recording its
	// end. The feature-start gate applies the ledger rule instead.
	state, err := db.LiveCollision(database, "feat-ledger", "sess-caller", 2*time.Minute)
	if err != nil {
		t.Fatalf("LiveCollision: %v", err)
	}
	if state.HasLiveCollision {
		t.Error("a ledger claim has no heartbeat and must not be a heartbeat-live collision")
	}
}
