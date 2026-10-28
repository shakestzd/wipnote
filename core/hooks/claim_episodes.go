package hooks

import (
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
)

// rootSessionForClaimLedger resolves the root session that owns sessionID's
// shard by asking the CLAIM LEDGER ITSELF which shard holds an episode for this
// session. Returns "" when it cannot be resolved — callers must refuse to write
// rather than guess a shard.
//
// This replaced a lookup against agent_lineage_trace, a table the compatibility
// projection never populates (feat-fc3cc9e0). That lookup therefore always
// found nothing and always took its `return sessionID` fallback, which for a
// subagent names a shard the session does not own. Because the ledger is
// canonical git-tracked state, that was a durable mis-write, not a lost
// derived-index update — the failure mode this whole cutover has to avoid.
//
// The ledger answers the question exactly: an episode records both SessionID
// and RootSessionID, so the shard that holds this session's episodes is found
// rather than inferred. Newest-first, so a session that appears under more than
// one root resolves to its most recent.
func rootSessionForClaimLedger(wipnoteDir, sessionID string) string {
	if wipnoteDir == "" || sessionID == "" {
		return ""
	}
	episodes, err := claimledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return ""
	}
	for i := len(episodes) - 1; i >= 0; i-- {
		e := episodes[i]
		if e.SessionID != sessionID {
			continue
		}
		if e.RootSessionID != "" {
			return e.RootSessionID
		}
		// An episode with no recorded root is its own root.
		return sessionID
	}
	return ""
}

// CloseClaimEpisodesForSession closes every open episode of sessionID, and no
// other session's, with outcome. It is the session-scoped close a delegate
// verb runs for a lost worker (plan-28b4a7f6 slice 16): the worker's pane is
// gone and no SessionEnd will fire, so its episodes close as expired. Each
// shard that holds an open episode of the session is closed in one
// read-modify-write, scoped to the session's own rows so siblings sharing a
// root keep theirs. Returns how many episodes it closed.
func CloseClaimEpisodesForSession(wipnoteDir, sessionID string, outcome claimledger.Outcome) (int, error) {
	if wipnoteDir == "" || sessionID == "" {
		return 0, nil
	}
	store := claimledger.NewStore(wipnoteDir)
	episodes, err := store.ReadAll()
	if err != nil {
		return 0, err
	}
	roots := map[string]bool{}
	for _, e := range episodes {
		if e.SessionID != sessionID || !e.IsOpen() {
			continue
		}
		root := e.RootSessionID
		if root == "" {
			root = sessionID
		}
		roots[root] = true
	}
	closed := 0
	now := time.Now().UTC()
	for root := range roots {
		n, err := store.CloseAllForSession(root, sessionID, outcome, now)
		closed += n
		if err != nil {
			return closed, err
		}
	}
	return closed, nil
}
