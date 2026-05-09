package main

import (
	"database/sql"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/models"
)

// openTreeTestDB creates an in-memory database with schema and a test session.
func openTreeTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := db.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	now := time.Now().UTC()
	sess := &models.Session{
		SessionID:     "sess-test",
		AgentAssigned: "claude-code",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, sess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	return database
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

func TestBuildEventTree_SuppressesDuplicateAgentRows(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)

	// Insert UserQuery anchor.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"uq-1", "claude-code", "tool_call", ts, "UserQuery", "sess-test", "recorded")

	// Insert task_delegation/Task as child of UserQuery.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, parent_event_id, subagent_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"td-1", "claude-code", "task_delegation", ts, "Task", "sess-test", "recorded", "uq-1", "researcher")

	// Insert duplicate tool_call/Agent as sibling of task_delegation.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, parent_event_id, subagent_type)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"tc-dup", "claude-code", "tool_call", ts, "Agent", "sess-test", "recorded", "uq-1", "researcher")

	// Insert child Bash/Read/Edit under task_delegation.
	for i, tool := range []string{"Bash", "Read", "Edit"} {
		mustExec(t, database,
			`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, parent_event_id)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			"child-"+string(rune('a'+i)), "claude-code", "tool_call", ts, tool, "sess-test", "recorded", "td-1")
	}

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("got %d turns, want 1", len(turns))
	}

	children := turns[0].Children
	// Should have 1 child (task_delegation) — the tool_call/Agent duplicate is suppressed.
	if len(children) != 1 {
		t.Fatalf("got %d children, want 1 (duplicate Agent row should be suppressed)", len(children))
	}

	td := children[0]
	if td["event_type"] != "task_delegation" {
		t.Errorf("child event_type = %v, want task_delegation", td["event_type"])
	}
	if td["tool_name"] != "Task" {
		t.Errorf("child tool_name = %v, want Task", td["tool_name"])
	}

	// task_delegation should have 3 nested children (Bash, Read, Edit).
	nested, ok := td["children"].([]map[string]any)
	if !ok {
		t.Fatalf("task_delegation children type = %T, want []map[string]any", td["children"])
	}
	if len(nested) != 3 {
		t.Fatalf("got %d nested children, want 3", len(nested))
	}
	tools := map[string]bool{}
	for _, c := range nested {
		tn, _ := c["tool_name"].(string)
		tools[tn] = true
	}
	for _, want := range []string{"Bash", "Read", "Edit"} {
		if !tools[want] {
			t.Errorf("missing nested child tool_name %q", want)
		}
	}
}

func TestBuildEventTree_NoDelegation_KeepsAgentRows(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)

	// Insert UserQuery.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"uq-2", "claude-code", "tool_call", ts, "UserQuery", "sess-test", "recorded")

	// Insert tool_call/Agent without any sibling task_delegation — should be kept.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, parent_event_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"tc-agent", "claude-code", "tool_call", ts, "Agent", "sess-test", "recorded", "uq-2")

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}

	// Find the turn for uq-2.
	var found *turn
	for i := range turns {
		if uq, ok := turns[i].UserQuery["event_id"].(string); ok && uq == "uq-2" {
			found = &turns[i]
			break
		}
	}
	if found == nil {
		t.Fatal("turn for uq-2 not found")
	}
	if len(found.Children) != 1 {
		t.Fatalf("got %d children, want 1 (Agent row should be kept when no delegation sibling)", len(found.Children))
	}
	if found.Children[0]["tool_name"] != "Agent" {
		t.Errorf("child tool_name = %v, want Agent", found.Children[0]["tool_name"])
	}
}

func TestBuildEventTree_OtelPrimary(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	tsMicros := time.Now().UTC().UnixMicro()

	// Insert an interaction span — OTel turn anchor.
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-interaction-1", "claude-code", "sess-test",
		"span", "interaction", "interaction",
		tsMicros, "trace-abc", "span-interaction-1",
		`{"user_prompt":"What is the capital of France?"}`)

	// Insert a tool_result span in the same trace.
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, parent_span, tool_name, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-tool-1", "claude-code", "sess-test",
		"span", "tool_result", "tool_result",
		tsMicros+1000, "trace-abc", "span-tool-1", "span-interaction-1",
		"Bash", "{}")

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("got %d turns, want 1", len(turns))
	}

	uq := turns[0].UserQuery
	if uq["event_id"] != "sig-interaction-1" {
		t.Errorf("event_id = %v, want sig-interaction-1", uq["event_id"])
	}
	if uq["tool_name"] != "UserQuery" {
		t.Errorf("tool_name = %v, want UserQuery", uq["tool_name"])
	}
	if uq["input_summary"] != "What is the capital of France?" {
		t.Errorf("input_summary = %v, want prompt text", uq["input_summary"])
	}
	if uq["session_id"] != "sess-test" {
		t.Errorf("session_id = %v, want sess-test", uq["session_id"])
	}
	if turns[0].Stats.ToolCount != 1 {
		t.Errorf("stats.tool_count = %d, want 1", turns[0].Stats.ToolCount)
	}
}

func TestBuildEventTree_OtelFallsBackToHook(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	ts := now.Format(time.RFC3339)

	// Insert only a hook UserQuery — no OTel interaction spans.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"uq-hook", "", "tool_call", ts, "UserQuery", "sess-test", "recorded")

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("got %d turns, want 1", len(turns))
	}

	uq := turns[0].UserQuery
	if uq["event_id"] != "uq-hook" {
		t.Errorf("event_id = %v, want uq-hook (hook fallback)", uq["event_id"])
	}
	if uq["agent_id"] != "claude-code" {
		t.Errorf("agent_id = %v, want claude-code from session fallback", uq["agent_id"])
	}
}

func TestBuildEventTree_OtelPromptFromTrace(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	tsMicros := time.Now().UTC().UnixMicro()

	// Interaction span with no prompt in attrs_json.
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-interaction-2", "claude-code", "sess-test",
		"span", "interaction", "interaction",
		tsMicros, "trace-xyz", "span-i2", `{}`)

	// user_prompt log in the same trace.
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-prompt-2", "claude-code", "sess-test",
		"log", "user_prompt", "user_prompt",
		tsMicros-500, "trace-xyz", "span-prompt-2", `{"prompt":"Hello world"}`)

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}
	if len(turns) != 1 {
		t.Fatalf("got %d turns, want 1", len(turns))
	}

	if turns[0].UserQuery["input_summary"] != "Hello world" {
		t.Errorf("input_summary = %v, want 'Hello world' from trace user_prompt log",
			turns[0].UserQuery["input_summary"])
	}
	if turns[0].UserQuery["agent_id"] != "claude-code" {
		t.Errorf("agent_id = %v, want claude-code from OTel harness", turns[0].UserQuery["agent_id"])
	}
}

func TestBuildEventTree_OtelLogOnlyCodexTurn(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	codexSess := &models.Session{
		SessionID:     "sess-codex-log-only",
		AgentAssigned: "codex",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, codexSess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	promptTS := now.UnixMicro()
	apiTS := now.Add(150 * time.Millisecond).UnixMicro()
	decisionTS := now.Add(300 * time.Millisecond).UnixMicro()
	toolTS := now.Add(450 * time.Millisecond).UnixMicro()

	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-prompt", "codex", "sess-codex-log-only", "prompt-1",
		"log", "user_prompt", "codex.user_prompt", 0, "trace-codex-1",
		`{"prompt":"fix the dashboard activity page","event.timestamp":"`+time.UnixMicro(promptTS).UTC().Format(time.RFC3339)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, model, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-api", "codex", "sess-codex-log-only", "prompt-1",
		"log", "api_request", "codex.api_request", 0, "trace-codex-1", "gpt-5.1",
		`{"model":"gpt-5.1","input_token_count":123,"output_token_count":45,"event.timestamp":"`+time.UnixMicro(apiTS).UTC().Format(time.RFC3339Nano)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, tool_name, decision, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-decision", "codex", "sess-codex-log-only", "prompt-1",
		"log", "tool_decision", "codex.tool_decision", 0, "trace-codex-1", "Bash", "allowed",
		`{"tool_name":"Bash","decision":"allowed","event.timestamp":"`+time.UnixMicro(decisionTS).UTC().Format(time.RFC3339Nano)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, tool_name, success, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-tool", "codex", "sess-codex-log-only", "prompt-1",
		"log", "tool_result", "codex.tool_result", 0, "trace-codex-1", "Bash", 1,
		`{"tool_name":"Bash","full_command":"go test ./cmd/wipnote","event.timestamp":"`+time.UnixMicro(toolTS).UTC().Format(time.RFC3339Nano)+`"}`)

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}

	var codexTurn *turn
	for i := range turns {
		if id, _ := turns[i].UserQuery["event_id"].(string); id == "sig-codex-prompt" {
			codexTurn = &turns[i]
			break
		}
	}
	if codexTurn == nil {
		t.Fatalf("Codex log-only prompt missing from event tree: %+v", turns)
	}

	if got := codexTurn.UserQuery["input_summary"]; got != "fix the dashboard activity page" {
		t.Fatalf("input_summary = %v, want prompt text", got)
	}
	if got := codexTurn.UserQuery["timestamp"]; got != time.UnixMicro(promptTS).UTC().Format(time.RFC3339) {
		t.Fatalf("timestamp = %v, want event timestamp", got)
	}
	if codexTurn.Stats.ToolCount != 3 {
		t.Fatalf("stats.tool_count = %d, want 3", codexTurn.Stats.ToolCount)
	}
	if len(codexTurn.Children) != 3 {
		t.Fatalf("children = %d, want 3 log-derived rows", len(codexTurn.Children))
	}

	if codexTurn.Children[0]["event_id"] != "sig-codex-tool" {
		t.Fatalf("first child = %v, want newest tool_result row", codexTurn.Children[0]["event_id"])
	}
	if codexTurn.Children[1]["tool_name"] != "Bash" {
		t.Fatalf("second child tool_name = %v, want Bash tool_decision row", codexTurn.Children[1]["tool_name"])
	}
	if codexTurn.Children[2]["tool_name"] != "api_request" {
		t.Fatalf("third child tool_name = %v, want api_request row", codexTurn.Children[2]["tool_name"])
	}
}

func TestBuildEventTree_OtelLogOnlyCodexTurn_BoundsChildrenByPromptInterval(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	codexSess := &models.Session{
		SessionID:     "sess-codex-log-window",
		AgentAssigned: "codex",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, codexSess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	prompt1TS := now.Add(-2 * time.Minute)
	child1TS := prompt1TS.Add(200 * time.Millisecond)
	prompt2TS := now.Add(-1 * time.Minute)
	child2TS := prompt2TS.Add(200 * time.Millisecond)
	child2ErrTS := prompt2TS.Add(400 * time.Millisecond)

	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-prompt-1", "codex", "sess-codex-log-window", "prompt-1",
		"log", "user_prompt", "codex.user_prompt", 0, "trace-codex-window-1",
		`{"prompt":"please run the dashboard server","event.timestamp":"`+prompt1TS.Format(time.RFC3339)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, tool_name, success, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-tool-1", "codex", "sess-codex-log-window", "",
		"log", "tool_result", "codex.tool_result", 0, "trace-codex-window-1", "Bash", 1,
		`{"tool_name":"Bash","full_command":"wipnote serve --port 8088","event.timestamp":"`+child1TS.Format(time.RFC3339Nano)+`"}`)

	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-prompt-2", "codex", "sess-codex-log-window", "prompt-2",
		"log", "user_prompt", "codex.user_prompt", 0, "trace-codex-window-2",
		`{"prompt":"codex events are still not showing","event.timestamp":"`+prompt2TS.Format(time.RFC3339)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, tool_name, success, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-tool-2", "codex", "sess-codex-log-window", "",
		"log", "tool_result", "codex.tool_result", 0, "trace-codex-window-2", "Read", 0,
		`{"tool_name":"Read","target":"cmd/wipnote/api_tree.go","event.timestamp":"`+child2TS.Format(time.RFC3339Nano)+`"}`)
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-codex-api-error-2", "codex", "sess-codex-log-window", "",
		"log", "api_error", "codex.api_error", 0, "trace-codex-window-2",
		`{"message":"dashboard query failed","event.timestamp":"`+child2ErrTS.Format(time.RFC3339Nano)+`"}`)

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}

	var prompt1Turn, prompt2Turn *turn
	for i := range turns {
		switch turns[i].UserQuery["event_id"] {
		case "sig-codex-prompt-1":
			prompt1Turn = &turns[i]
		case "sig-codex-prompt-2":
			prompt2Turn = &turns[i]
		}
	}
	if prompt1Turn == nil || prompt2Turn == nil {
		t.Fatalf("expected both Codex prompt turns, got %+v", turns)
	}

	if len(prompt1Turn.Children) != 1 {
		t.Fatalf("prompt1 children = %d, want 1", len(prompt1Turn.Children))
	}
	if got := prompt1Turn.Children[0]["event_id"]; got != "sig-codex-tool-1" {
		t.Fatalf("prompt1 child event_id = %v, want sig-codex-tool-1", got)
	}
	if prompt1Turn.Stats.ToolCount != 1 || prompt1Turn.Stats.ErrorCount != 0 {
		t.Fatalf("prompt1 stats = %+v, want tool_count=1 error_count=0", prompt1Turn.Stats)
	}

	if len(prompt2Turn.Children) != 2 {
		t.Fatalf("prompt2 children = %d, want 2", len(prompt2Turn.Children))
	}
	if got := prompt2Turn.Children[0]["event_id"]; got != "sig-codex-api-error-2" {
		t.Fatalf("prompt2 newest child event_id = %v, want sig-codex-api-error-2", got)
	}
	if got := prompt2Turn.Children[1]["event_id"]; got != "sig-codex-tool-2" {
		t.Fatalf("prompt2 second child event_id = %v, want sig-codex-tool-2", got)
	}
	if prompt2Turn.Stats.ToolCount != 2 || prompt2Turn.Stats.ErrorCount != 2 {
		t.Fatalf("prompt2 stats = %+v, want tool_count=2 error_count=2", prompt2Turn.Stats)
	}
}

func TestBuildEventTree_OtelLogOnlyCodexTurn_SortsZeroMicrosPromptsByEventTimestamp(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	now := time.Now().UTC()
	codexSess := &models.Session{
		SessionID:     "sess-codex-zero-sort",
		AgentAssigned: "codex",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, codexSess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	for i := 0; i < 4; i++ {
		ts := now.Add(time.Duration(i) * time.Minute)
		mustExec(t, database,
			`INSERT INTO otel_signals
			 (signal_id, harness, session_id, prompt_id, kind, canonical, native, ts_micros, trace_id, attrs_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("sig-codex-zero-sort-%d", i), "codex", "sess-codex-zero-sort", fmt.Sprintf("prompt-%d", i),
			"log", "user_prompt", "codex.user_prompt", 0, fmt.Sprintf("trace-codex-zero-sort-%d", i),
			`{"prompt":"prompt `+strconv.Itoa(i)+`","event.timestamp":"`+ts.Format(time.RFC3339Nano)+`"}`)
	}

	turns, err := buildEventTree(database, 2)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}

	var prompts []string
	for _, turn := range turns {
		if turn.SessionID == "sess-codex-zero-sort" {
			if prompt, _ := turn.UserQuery["input_summary"].(string); prompt != "" {
				prompts = append(prompts, prompt)
			}
		}
	}
	if len(prompts) != 2 {
		t.Fatalf("zero-sort prompts = %v, want 2 newest prompts", prompts)
	}
	if prompts[0] != "prompt 3" || prompts[1] != "prompt 2" {
		t.Fatalf("zero-sort prompts = %v, want [prompt 3 prompt 2]", prompts)
	}
}

// TestBuildEventTree_MixedOtelAndHook verifies the merge logic: OTel-anchored
// sessions, hook-only sessions, and the regression case where a session emits
// both an OTel interaction span AND a hook UserQuery row with empty step_id
// (which used to surface the hook row as a duplicate prompt).
func TestBuildEventTree_MixedOtelAndHook(t *testing.T) {
	database := openTreeTestDB(t)
	defer database.Close()

	// Second session for the hook-only case.
	now := time.Now().UTC()
	hookOnlySess := &models.Session{
		SessionID:     "sess-hook-only",
		AgentAssigned: "claude-code",
		CreatedAt:     now,
		Status:        "active",
	}
	if err := db.InsertSession(database, hookOnlySess); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	// Three turns at distinct timestamps so DESC sort is unambiguous.
	otelTs := now.Add(-30 * time.Second)
	hookOnlyTs := now.Add(-20 * time.Second)
	dupHookTs := now.Add(-10 * time.Second)

	// 1. OTel interaction span with a paired hook UserQuery whose step_id
	//    matches the trace_id (the well-formed case).
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-i1", "claude-code", "sess-test",
		"span", "interaction", "interaction",
		otelTs.UnixMicro(), "trace-otel-1", "span-i1",
		`{"user_prompt":"first prompt"}`)
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, step_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"uq-otel-1", "claude-code", "tool_call", otelTs.Format(time.RFC3339),
		"UserQuery", "sess-test", "recorded", "trace-otel-1")

	// 2. Hook-only session — no OTel anywhere. Must be retained.
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"uq-hook-only", "claude-code", "tool_call", hookOnlyTs.Format(time.RFC3339),
		"UserQuery", "sess-hook-only", "recorded")

	// 3. Regression: a session that has an OTel interaction span AND a hook
	//    UserQuery row with EMPTY step_id near the same timestamp. The
	//    session+timestamp window check must dedup it.
	mustExec(t, database,
		`INSERT INTO otel_signals
		 (signal_id, harness, session_id, kind, canonical, native, ts_micros, trace_id, span_id, attrs_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"sig-i2", "claude-code", "sess-test",
		"span", "interaction", "interaction",
		dupHookTs.UnixMicro(), "trace-otel-2", "span-i2",
		`{"user_prompt":"third prompt"}`)
	mustExec(t, database,
		`INSERT INTO agent_events (event_id, agent_id, event_type, timestamp, tool_name, session_id, status, step_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"uq-dup-empty-step", "claude-code", "tool_call", dupHookTs.Format(time.RFC3339),
		"UserQuery", "sess-test", "recorded", "")

	turns, err := buildEventTree(database, 50)
	if err != nil {
		t.Fatalf("buildEventTree: %v", err)
	}

	// Expected: two OTel turns (sig-i1, sig-i2) + one hook-only turn (uq-hook-only).
	// The empty-step hook row (uq-dup-empty-step) must be deduped.
	if len(turns) != 3 {
		var ids []string
		for _, t := range turns {
			if id, ok := t.UserQuery["event_id"].(string); ok {
				ids = append(ids, id)
			}
		}
		t.Fatalf("got %d turns, want 3 (event_ids=%v)", len(turns), ids)
	}

	for _, tn := range turns {
		if id, _ := tn.UserQuery["event_id"].(string); id == "uq-dup-empty-step" {
			t.Errorf("hook UserQuery with empty step_id was not deduped against the same-session OTel interaction span")
		}
	}

	// Sanity: hook-only session's turn is present.
	foundHookOnly := false
	for _, tn := range turns {
		if id, _ := tn.UserQuery["event_id"].(string); id == "uq-hook-only" {
			foundHookOnly = true
		}
	}
	if !foundHookOnly {
		t.Error("hook-only session's UserQuery turn missing — merge dropped unanchored hook turns")
	}
}

func TestComputeStats_CountsNestedChildren(t *testing.T) {
	children := []map[string]any{
		{
			"event_type": "task_delegation",
			"tool_name":  "Task",
			"status":     "recorded",
			"model":      "claude-sonnet",
			"children": []map[string]any{
				{"event_type": "tool_call", "tool_name": "Bash", "status": "recorded", "model": "claude-sonnet"},
				{"event_type": "error", "tool_name": "Read", "status": "failed", "model": "claude-sonnet"},
			},
		},
	}

	stats := computeStats(children)
	if stats.ToolCount != 3 {
		t.Errorf("ToolCount = %d, want 3", stats.ToolCount)
	}
	if stats.ErrorCount != 1 {
		t.Errorf("ErrorCount = %d, want 1", stats.ErrorCount)
	}
}
