package hooks

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

// claimForAgent opens a claim on item for agentID in runsSession, started at
// offset after a fixed base so the ledger order is deterministic.
func claimForAgent(t *testing.T, wipnoteDir, agentID, item string, offset time.Duration) {
	t.Helper()
	base := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	_, _, err := claimledger.NewStore(wipnoteDir).Open(runsSession, claimledger.Episode{WorkItemID: item,
		SessionID: runsSession, RootSessionID: runsSession, AgentID: agentID, StartedAt: base.Add(offset)})
	if err != nil {
		t.Fatal(err)
	}
}

// recordAgentRun records one background Bash run from agentID ("" for the
// root) and returns the work item the run was credited to.
func recordAgentRun(t *testing.T, projectDir, wipnoteDir, agentID, toolUseID string) string {
	t.Helper()
	ev := backgroundBash(projectDir, toolUseID, "task-"+toolUseID, "gate")
	ev.AgentID = agentID
	PostToolUseRecord(ev)
	for _, r := range runRecords(t, wipnoteDir, false) {
		if r.TaskID == "task-"+toolUseID {
			return r.WorkItem
		}
	}
	t.Fatalf("no run recorded for %s", toolUseID)
	return ""
}

// bug-b0bbd985: with several agents in one session holding different claims,
// each run is credited to the calling agent's own claim, not the newest claim
// in the session.
func TestRuns_CreditTheCallingAgentsClaim(t *testing.T) {
	t.Setenv("WIPNOTE_AGENT_ID", "")
	projectDir, wipnoteDir := runsProject(t)
	claimForAgent(t, wipnoteDir, "agent-sub-one", "feat-a0000001", 0)
	claimForAgent(t, wipnoteDir, db.AgentRootSentinel, "feat-r0000001", time.Minute)
	claimForAgent(t, wipnoteDir, "agent-sub-two", "feat-b0000001", 2*time.Minute)

	cases := []struct{ agent, toolUse, want string }{
		{"", "toolu_root", "feat-r0000001"},
		{"agent-sub-one", "toolu_one", "feat-a0000001"},
		{"agent-sub-two", "toolu_two", "feat-b0000001"},
		// An agent with no claim of its own takes the session's (root) claim.
		{"agent-sub-none", "toolu_none", "feat-r0000001"},
	}
	for _, c := range cases {
		if got := recordAgentRun(t, projectDir, wipnoteDir, c.agent, c.toolUse); got != c.want {
			t.Errorf("agent %q run credited to %q, want %q", c.agent, got, c.want)
		}
	}
}

// A single agent holding the only claim keeps today's behaviour.
func TestRuns_SingleAgentClaimUnchanged(t *testing.T) {
	t.Setenv("WIPNOTE_AGENT_ID", "")
	projectDir, wipnoteDir := runsProject(t)
	claimForAgent(t, wipnoteDir, db.AgentRootSentinel, "feat-r0000002", 0)
	if got := recordAgentRun(t, projectDir, wipnoteDir, "", "toolu_solo"); got != "feat-r0000002" {
		t.Errorf("root run credited to %q, want feat-r0000002", got)
	}
}

// With no claim for the caller or the root, the session's newest open claim is
// still the fallback; with no claim at all the run has no work item.
func TestRuns_NoOwnClaimFallsBackToSession(t *testing.T) {
	t.Setenv("WIPNOTE_AGENT_ID", "")
	projectDir, wipnoteDir := runsProject(t)
	if got := recordAgentRun(t, projectDir, wipnoteDir, "", "toolu_bare"); got != "" {
		t.Errorf("unclaimed run credited to %q, want none", got)
	}
	claimForAgent(t, wipnoteDir, "agent-sub-one", "feat-a0000003", 0)
	if got := recordAgentRun(t, projectDir, wipnoteDir, "", "toolu_rootless"); got != "feat-a0000003" {
		t.Errorf("root run with no root claim credited to %q, want the session's feat-a0000003", got)
	}
}
