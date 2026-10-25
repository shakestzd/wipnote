package hooks

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// SubagentStop runs the transcript failures pass over the subagent's own
// transcript (agent_transcript_path), so a manual denial inside a subagent is
// recorded as failed (feat-ddeaa0d1). Claude Code gives a subagent's hooks the
// parent's session_id plus agent_id, so the subagent's start lines land in the
// parent session's tool log, which is the log the pass checks.

const testSubagentID = "agent-def456"

// startSubagentToolAt records a PreToolUse start fired inside a subagent.
func startSubagentToolAt(t *testing.T, h *splitDBHarness, toolUseID string, at time.Time) {
	t.Helper()
	prev := toolLogNow
	toolLogNow = func() time.Time { return at }
	defer func() { toolLogNow = prev }()
	h.runPre(t, &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Read",
		ToolUseID: toolUseID, ToolInput: map[string]any{"file_path": "a.go"},
		AgentID: testSubagentID, AgentType: "Explore", Harness: HarnessClaude})
}

// subagentStopEvent is a Claude Code SubagentStop payload for the test agent.
func subagentStopEvent(h *splitDBHarness, agentTranscript string) *CloudEvent {
	return &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, HookEventName: "SubagentStop",
		AgentID: testSubagentID, AgentType: "Explore", AgentTranscriptPath: agentTranscript,
		Harness: HarnessClaude}
}

// assertStartInLog checks the subagent's start line is in the parent
// session's tool log, tagged with the subagent's id.
func assertStartInLog(t *testing.T, h *splitDBHarness, toolUseID string) {
	t.Helper()
	data, err := os.ReadFile(canonicalToolLogPath(h.projectDir, h.sessionID))
	if err != nil {
		t.Fatalf("read tool log: %v", err)
	}
	id := toolCallEventID(h.sessionID, toolUseID)
	for _, line := range strings.Split(string(data), "\n") {
		var ev canonicalToolEvent
		if json.Unmarshal([]byte(line), &ev) == nil && ev.ID == id && ev.Tool != "" {
			if ev.Agent != testSubagentID {
				t.Errorf("start line agent = %q, want %q", ev.Agent, testSubagentID)
			}
			return
		}
	}
	t.Fatalf("no start line for %s in the parent session's tool log", toolUseID)
}

// TestSubagentStop_TranscriptDenialCountsAsFailed: a call denied inside a
// subagent has an error tool_result only in the subagent's own transcript.
// SubagentStop records it failed, and the sweep does not call it aborted.
func TestSubagentStop_TranscriptDenialCountsAsFailed(t *testing.T) {
	h := newSplitDBHarness(t, "subagent-denied-1")
	now := time.Now().UTC()
	startSubagentToolAt(t, h, "tu_sub_denied", now.Add(-10*time.Minute))
	assertStartInLog(t, h, "tu_sub_denied")
	tp := writeTranscript(t, t.TempDir(), 0, toolResult("tu_sub_denied", true, []any{map[string]any{
		"type": "text", "text": "The user doesn't want to proceed with this tool use. The tool use was rejected."}}))
	if _, err := SubagentStop(subagentStopEvent(h, tp)); err != nil {
		t.Fatalf("SubagentStop: %v", err)
	}
	if got := completionsFor(t, h.projectDir, h.sessionID, "tu_sub_denied"); strings.Join(got, ",") != "failed" {
		t.Fatalf("after SubagentStop completions = %v, want [failed]", got)
	}
	markOwnerDead(t, h)
	sweepAndCheck(t, h, now, map[string][]string{"tu_sub_denied": {"failed"}})
	if abortedInHTML(t, h.projectDir, h.sessionID, "tu_sub_denied") {
		t.Error("subagent denial was recorded as aborted")
	}
}

// TestSubagentStop_SuccessUntouched: a subagent call that succeeded (finished,
// or with its async completion still in flight) gets nothing from SubagentStop.
func TestSubagentStop_SuccessUntouched(t *testing.T) {
	h := newSplitDBHarness(t, "subagent-ok-1")
	old := time.Now().UTC().Add(-time.Minute)
	startSubagentToolAt(t, h, "tu_sub_done", old)
	startSubagentToolAt(t, h, "tu_sub_inflight", old)
	finishTool(t, h, h.sessionID, "tu_sub_done")
	tp := writeTranscript(t, t.TempDir(), 0,
		toolResult("tu_sub_done", false, "ok"), toolResult("tu_sub_inflight", false, "fine"))
	if _, err := SubagentStop(subagentStopEvent(h, tp)); err != nil {
		t.Fatalf("SubagentStop: %v", err)
	}
	if got := completionsFor(t, h.projectDir, h.sessionID, "tu_sub_done"); strings.Join(got, ",") != "completed" {
		t.Errorf("finished call completions = %v, want [completed]", got)
	}
	if got := completionsFor(t, h.projectDir, h.sessionID, "tu_sub_inflight"); len(got) != 0 {
		t.Errorf("SubagentStop wrote %v for a successful call", got)
	}
}

// TestSubagentStop_NoAgentTranscriptIsNoOp: a payload with no
// agent_transcript_path writes nothing and returns no error, even when the
// main transcript_path holds an error result for an open call.
func TestSubagentStop_NoAgentTranscriptIsNoOp(t *testing.T) {
	h := newSplitDBHarness(t, "subagent-nopath-1")
	startSubagentToolAt(t, h, "tu_sub_open", time.Now().UTC().Add(-time.Minute))
	ev := subagentStopEvent(h, "")
	ev.TranscriptPath = writeTranscript(t, t.TempDir(), 0, toolResult("tu_sub_open", true, "rejected"))
	res, err := SubagentStop(ev)
	if err != nil || res == nil || !res.Continue {
		t.Fatalf("SubagentStop = %+v, %v; want continue, no error", res, err)
	}
	if got := completionsFor(t, h.projectDir, h.sessionID, "tu_sub_open"); len(got) != 0 {
		t.Errorf("no agent transcript, yet SubagentStop wrote %v", got)
	}
}

// TestSubagentStop_PayloadField pins the payload field name Claude Code
// documents for the subagent's own transcript.
func TestSubagentStop_PayloadField(t *testing.T) {
	var ev CloudEvent
	raw := `{"session_id":"s","hook_event_name":"SubagentStop","agent_id":"def456",` +
		`"agent_transcript_path":"/x/abc123/subagents/agent-def456.jsonl"}`
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		t.Fatal(err)
	}
	if ev.AgentTranscriptPath != "/x/abc123/subagents/agent-def456.jsonl" {
		t.Errorf("AgentTranscriptPath = %q", ev.AgentTranscriptPath)
	}
}
