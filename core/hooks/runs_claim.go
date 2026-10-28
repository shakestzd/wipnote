package hooks

import (
	"path/filepath"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// runWorkItem is the active work item for the call (bug-b0bbd985): a claim
// already resolved onto ctx, else the CALLING agent's own open claim, else the
// session's root claim, else the session's newest open claim. Several agents
// share one Claude Code session, so the session's newest claim alone would
// credit one agent's run to another agent's work item.
func runWorkItem(ctx *toolUseContext) string {
	if item := firstNonEmpty(ctx.ClaimedItem, ctx.FeatureID); item != "" {
		return item
	}
	if ctx.ProjectDir == "" {
		return ""
	}
	wipnoteDir := filepath.Join(ctx.ProjectDir, ".wipnote")
	for _, agentID := range runClaimAgents(ctx) {
		if item := canonicalAgentOpenClaim(wipnoteDir, ctx.SessionID, agentID); item != "" {
			return item
		}
	}
	return canonicalActiveWorkItem(ctx.ProjectDir, ctx.SessionID)
}

// runClaimAgents lists the claim-ledger agent ids to try, most specific first.
// A subagent's claims carry its hook agent_id; the top-level owner's carry the
// root sentinel (or a launcher id such as "codex", which ctx.AgentID holds).
func runClaimAgents(ctx *toolUseContext) []string {
	if ctx.IsSubagent && ctx.AgentID != "" {
		return []string{ctx.AgentID, db.AgentRootSentinel}
	}
	return []string{db.AgentRootSentinel, ctx.AgentID}
}

// canonicalAgentOpenClaim is the work item of the newest open claim agentID
// holds in sessionID, or "".
func canonicalAgentOpenClaim(wipnoteDir, sessionID, agentID string) string {
	if agentID == "" {
		return ""
	}
	ep, _ := scanOpenClaims(wipnoteDir, sessionID, func(e claimledger.Episode, _ string) bool {
		return e.SessionID == sessionID && e.AgentID == agentID
	})
	return ep.WorkItemID
}
