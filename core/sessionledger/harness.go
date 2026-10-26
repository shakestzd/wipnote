package sessionledger

import (
	"errors"
	"strings"
)

// agentFallbackID is the identity core/agent.Detect reports for a process that
// no harness marks: a plain shell, a delegate script, a herdr pane's own
// shell. It is an AGENT identity that names nobody's harness, so a row tagged
// with it misfiles a real session under a harness that does not exist and
// every harness-filtered reader misses it (bug-5333ea49).
const agentFallbackID = "human"

// ErrAgentHarness is returned by Open when a new row's harness is the agent
// fallback identity instead of a harness. A caller that does not know the
// harness leaves the row to the SessionStart hook, which does.
var ErrAgentHarness = errors.New("sessionledger: agent fallback identity is not a harness")

// isAgentFallback reports whether harness is the agent fallback identity.
func isAgentFallback(harness string) bool {
	return strings.EqualFold(strings.TrimSpace(harness), agentFallbackID)
}

// HarnessFromAgent maps an agent identity (a session record's data-agent) to
// the harness tag a ledger row carries — the same tag SessionStart writes.
// Anything that names no harness, the "human" fallback included, maps to ""
// (unknown), never to itself.
func HarnessFromAgent(agentID string) string {
	switch a := strings.ToLower(strings.TrimSpace(agentID)); a {
	case "claude-code", "claude":
		return "claude"
	case "codex", "gemini", "antigravity":
		return a
	}
	return ""
}

// IsHarness reports whether harness is a real harness tag — the closed set
// SessionStart writes — rather than empty or an agent identity.
func IsHarness(harness string) bool {
	return harness != "" && HarnessFromAgent(harness) == harness
}
