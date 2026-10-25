package hooks

import "testing"

func TestIsSubagentEvent_GenericHarnessIDs(t *testing.T) {
	for _, agentID := range []string{"", "claude-code", "claude", "codex", "gemini", "antigravity"} {
		t.Run(agentID, func(t *testing.T) {
			if isSubagentEvent(&CloudEvent{AgentID: agentID}) {
				t.Fatalf("isSubagentEvent(%q) = true, want false", agentID)
			}
		})
	}

	if !isSubagentEvent(&CloudEvent{AgentID: "feature-coder"}) {
		t.Fatal("isSubagentEvent(\"feature-coder\") = false, want true")
	}
}

// TestIsSubagentEvent_CodexRealSubagentIdentity is bug-fa036758's sibling
// regression test for the subagent-detection side. Before feat-b7bc4267,
// every Codex event's AgentID was hardcoded to the literal "codex" and this
// always returned false — no Codex event was ever detected as a subagent.
// A real Codex subagent identity (a thread ID, per the live capture in
// feat-b7bc4267's research) must now be detected as a subagent with no
// change to this function itself — the fix was entirely upstream, in
// parseCodexEvent no longer discarding the payload's real agent_id.
func TestIsSubagentEvent_CodexRealSubagentIdentity(t *testing.T) {
	realCodexSubagentID := "019fe188-03aa-7b92-83dc-0b4dc62e0014" // shape captured live against codex-cli 0.147.0
	if !isSubagentEvent(&CloudEvent{Harness: HarnessCodex, AgentID: realCodexSubagentID}) {
		t.Fatalf("isSubagentEvent(%q) = false, want true (a real Codex subagent identity must be detected as a subagent)", realCodexSubagentID)
	}
	// The root/orchestrator case must remain unaffected: parseCodexEvent's
	// generic fallback constant is still correctly treated as non-subagent.
	if isSubagentEvent(&CloudEvent{Harness: HarnessCodex, AgentID: codexGenericAgentID}) {
		t.Fatalf("isSubagentEvent(%q) = true, want false (Codex's root/orchestrator case must not be misdetected as a subagent)", codexGenericAgentID)
	}
}
