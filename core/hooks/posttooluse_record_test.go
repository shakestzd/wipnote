package hooks

import (
	"database/sql"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

func TestDurationMS_ParsesFromPayloads(t *testing.T) {
	cases := []struct {
		name    string
		harness Harness
		raw     string
		want    time.Duration
	}{
		{"claude int", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":1500}`, 1500 * time.Millisecond},
		{"claude float", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":12.9}`, 12 * time.Millisecond},
		{"claude string", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":"250"}`, 250 * time.Millisecond},
		{"claude absent", HarnessClaude, `{"session_id":"s","tool_name":"Bash"}`, 0},
		{"claude null", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":null}`, 0},
		{"claude garbage tolerated", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":"soon"}`, 0},
		{"claude negative ignored", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":-5}`, 0},
		{"codex int", HarnessCodex, `{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_use_id":"c1","duration_ms":42}`, 42 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := ParseEventForHarness(tc.harness, []byte(tc.raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if ev.ToolName != "Bash" {
				t.Fatalf("payload not decoded (tool_name=%q)", ev.ToolName)
			}
			if got := ev.DurationMS.Duration(); got != tc.want {
				t.Errorf("duration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToolCallEventID_Deterministic(t *testing.T) {
	a := toolCallEventID("sess-1", "toolu_A")
	if a == "" || a != toolCallEventID("sess-1", "toolu_A") {
		t.Fatalf("id not deterministic: %q", a)
	}
	if a == toolCallEventID("sess-1", "toolu_B") {
		t.Error("different tool_use_id must yield a different id")
	}
	if a == toolCallEventID("sess-2", "toolu_A") {
		t.Error("different session must yield a different id")
	}
	if toolCallEventID("sess-1", "") != "" || toolCallEventID("", "toolu_A") != "" {
		t.Error("empty input must yield empty id")
	}
	if r := toolCallEventIDOrRandom("sess-1", ""); r == "" || r == toolCallEventIDOrRandom("sess-1", "") {
		t.Error("random fallback must be non-empty and unique")
	}
}

func TestBuildToolCompletionEvent_StartIsNowMinusDuration(t *testing.T) {
	t.Setenv("WIPNOTE_PARENT_PROMPT_EVENT", "evt-parent")
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	ctx := &toolUseContext{SessionID: "s", AgentID: "claude-code", ParentEventID: "evt-other"}

	ev := buildToolCompletionEvent(&CloudEvent{ToolName: "Bash", ToolUseID: "t1", DurationMS: 3000}, ctx, "completed", "", now)
	if !ev.Timestamp.Equal(now.Add(-3 * time.Second)) {
		t.Errorf("start = %v, want now-3s", ev.Timestamp)
	}
	if ev.ExecDuration != 3 {
		t.Errorf("exec duration = %v, want 3", ev.ExecDuration)
	}
	if ev.ParentEventID != "evt-parent" {
		t.Errorf("parent = %q, want WIPNOTE_PARENT_PROMPT_EVENT value", ev.ParentEventID)
	}
	if ev.EventID != toolCallEventID("s", "t1") {
		t.Errorf("event id = %q, want deterministic id", ev.EventID)
	}

	ev = buildToolCompletionEvent(&CloudEvent{ToolName: "Bash", ToolUseID: "t1"}, ctx, "failed", "", now)
	if !ev.Timestamp.Equal(now) {
		t.Errorf("start without duration = %v, want now", ev.Timestamp)
	}
}

type eventRow struct {
	status   string
	stepID   string
	duration float64
	parent   sql.NullString
}

func readEventRow(t *testing.T, database *sql.DB, eventID string) (eventRow, bool) {
	t.Helper()
	var r eventRow
	err := database.QueryRow(`SELECT status, COALESCE(step_id,''), execution_duration_seconds, parent_event_id
		FROM agent_events WHERE event_id = ?`, eventID).Scan(&r.status, &r.stepID, &r.duration, &r.parent)
	if err == sql.ErrNoRows {
		return r, false
	}
	if err != nil {
		t.Fatalf("read event %s: %v", eventID, err)
	}
	return r, true
}

// TestPostToolUse_SplitDB_PairsParallelCallsByToolUseID runs two overlapping
// calls to the SAME tool through the production handle split and asserts each
// PostToolUse completes its own started row (by tool_use_id) in the daemon
// projection, with no extra rows.
func TestPostToolUse_SplitDB_PairsParallelCallsByToolUseID(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-pair-001")
	in := map[string]any{"file_path": "/tmp/x.go"}
	pre := func(id string) {
		h.runPre(t, &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Read", ToolUseID: id, ToolInput: in})
	}
	post := func(id string, isErr bool, durMS int64) {
		t.Helper()
		h.runPost(t, &CloudEvent{
			SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Read", ToolUseID: id, ToolInput: in,
			ToolResult: map[string]any{"output": "x", "is_error": isErr}, DurationMS: durationMS(durMS),
		})
	}

	pre("toolu_A")
	pre("toolu_B")
	for _, id := range []string{"toolu_A", "toolu_B"} {
		if r, ok := readEventRow(t, h.daemonDB, toolCallEventID(h.sessionID, id)); !ok || r.status != "started" {
			t.Fatalf("pre row for %s: ok=%v status=%q, want started", id, ok, r.status)
		}
	}

	// Complete in the opposite order: B (failed) first, then A.
	post("toolu_B", true, 2000)
	post("toolu_A", false, 0)

	a, _ := readEventRow(t, h.daemonDB, toolCallEventID(h.sessionID, "toolu_A"))
	b, _ := readEventRow(t, h.daemonDB, toolCallEventID(h.sessionID, "toolu_B"))
	if a.status != "completed" || a.stepID != "toolu_A" {
		t.Errorf("A = %+v, want completed/toolu_A", a)
	}
	if b.status != "failed" || b.stepID != "toolu_B" || b.duration != 2 {
		t.Errorf("B = %+v, want failed/toolu_B/2s", b)
	}

	var n int
	_ = h.daemonDB.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'Read'`, h.sessionID).Scan(&n)
	if n != 2 {
		t.Errorf("rows = %d, want 2 (upserts, not inserts)", n)
	}
	var diskN int
	_ = h.diskDB.QueryRow(`SELECT COUNT(*) FROM agent_events`).Scan(&diskN)
	if diskN != 0 {
		t.Errorf("disk handle got %d agent_events rows; completion must route through the daemon", diskN)
	}
}

// TestPostToolUse_SplitDB_WritesCompleteRowWithoutPre covers a PostToolUse
// whose started row never reached the daemon: the completion creates the full
// row itself, and an unknown parent id binds NULL instead of failing the FK.
func TestPostToolUse_SplitDB_WritesCompleteRowWithoutPre(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-nopre-001")

	parent := &models.AgentEvent{
		EventID: "evt-userquery-1", AgentID: "human", EventType: models.EventToolCall,
		Timestamp: time.Now().UTC(), ToolName: "UserQuery", SessionID: h.sessionID, Status: "recorded",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	if !routeSQLAsync(h.projectDir, agentEventInsertSQL, agentEventInsertArgs(parent)...) {
		t.Fatal("seed parent via daemon stub failed")
	}

	run := func(id, parentEnv string) eventRow {
		t.Helper()
		t.Setenv("WIPNOTE_PARENT_PROMPT_EVENT", parentEnv)
		h.runPost(t, &CloudEvent{
			SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Glob", ToolUseID: id,
			ToolInput: map[string]any{"pattern": "*.go"}, ToolResult: map[string]any{"output": "a.go"},
		})
		r, ok := readEventRow(t, h.daemonDB, toolCallEventID(h.sessionID, id))
		if !ok {
			t.Fatalf("completion row for %s missing from daemon projection", id)
		}
		return r
	}

	if r := run("toolu_known_parent", parent.EventID); r.status != "completed" || r.parent.String != parent.EventID {
		t.Errorf("known parent row = %+v, want completed with parent %s", r, parent.EventID)
	}
	if r := run("toolu_unknown_parent", "evt-not-in-daemon"); r.status != "completed" || r.parent.Valid {
		t.Errorf("unknown parent row = %+v, want completed with NULL parent", r)
	}
}
