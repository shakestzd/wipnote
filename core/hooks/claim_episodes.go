package hooks

import (
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
