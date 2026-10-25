package db

// AgentRootSentinel is stored as agent_id when WIPNOTE_AGENT_ID is unset,
// representing the top-level (non-subagent) session owner.
const AgentRootSentinel = "__root__"

// NormaliseAgentID returns agentID if non-empty, otherwise AgentRootSentinel.
// Use this before every call to GetActiveWorkItem.
func NormaliseAgentID(agentID string) string {
	if agentID == "" {
		return AgentRootSentinel
	}
	return agentID
}
