package hooks

// Regression tests for Finding 266 (roborev bug-60107613):
// hasPriorDelegationEvidence must NOT treat Codex's generic TaskStarted
// checkpoint as sidecar/delegation evidence; only real spawn events
// (Task, Agent, multi_agent.*) suppress the research advisory. The evidence
// comes from the session's canonical tool log.

import "testing"

func TestDelegationEvidence_OnlySpawnsSuppressResearchAdvisory(t *testing.T) {
	for _, tc := range []struct {
		tool         string
		wantAdvisory bool
	}{
		{"TaskStarted", true}, // Codex progress checkpoint, not a spawn
		{"Task", false},
		{"Agent", false},
		{"multi_agent.spawn", false},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			projectDir := canonGuardProject(t)
			logTools(projectDir, canonicalToolEvent{Tool: tc.tool})
			got := checkOrchestratorResearchDelegationAdvisory(
				&CloudEvent{ToolName: "web.search_query"},
				&toolUseContext{SessionID: canonGuardSession, ProjectDir: projectDir})
			if (got != "") != tc.wantAdvisory {
				t.Fatalf("after %s: advisory=%q, want advisory=%v", tc.tool, got, tc.wantAdvisory)
			}
		})
	}
}
