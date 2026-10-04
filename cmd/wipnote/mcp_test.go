package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

const secretPrompt = "SECRET-PROMPT-TEXT-do-not-leak"

var mcpTestNow = time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)

type sigSpec struct {
	canonical, kind, tool, toolUseID string
	cost                             *float64
	success                          *bool
	durationMs                       int64
	model, agentType                 string
	offset                           time.Duration
}

func fp(v float64) *float64 { return &v }
func bp(v bool) *bool       { return &v }

// writeShard writes one session's events.ndjson. Every line carries a secret
// string in attrs and error_msg so tests can prove it never reaches a result.
func writeShard(t *testing.T, wipnoteDir, id string, sigs []sigSpec) {
	t.Helper()
	dir := filepath.Join(wipnoteDir, "sessions", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for i, s := range sigs {
		line := map[string]any{
			"kind": s.kind, "harness": "claude", "session_id": "harness-" + id,
			"signal_id": fmt.Sprintf("%s-%d", id, i), "canonical": s.canonical, "native": "x",
			"ts":        mcpTestNow.Add(s.offset).Format(time.RFC3339Nano),
			"tool_name": s.tool, "tool_use_id": s.toolUseID,
			"duration_ms": s.durationMs, "error_msg": secretPrompt,
			"attrs": map[string]any{"prompt": secretPrompt, "tool_input": secretPrompt},
		}
		if s.model != "" {
			line["model"] = s.model
		}
		if s.agentType != "" {
			line["agent_type"] = s.agentType
		}
		if s.cost != nil {
			line["cost_usd"] = *s.cost
			line["tokens_input"], line["tokens_output"] = 100, 10
		}
		if s.success != nil {
			line["success"] = *s.success
		}
		raw, _ := json.Marshal(line)
		b.Write(raw)
		b.WriteByte('\n')
	}
	path := filepath.Join(dir, "events.ndjson")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mcpTestNow, mcpTestNow); err != nil {
		t.Fatal(err)
	}
}

func writeItem(t *testing.T, wipnoteDir, sub string, n *models.Node) {
	t.Helper()
	if _, err := workitem.WriteNodeHTML(filepath.Join(wipnoteDir, sub), n); err != nil {
		t.Fatal(err)
	}
}

func seedProject(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".wipnote")
	old := mcpTestNow.Add(-10 * 24 * time.Hour)
	writeItem(t, dir, "features", &models.Node{ID: "feat-aaaa0001", Type: "feature", Title: "Blocked\none " + strings.Repeat("x", 200), Status: models.StatusBlocked, CreatedAt: old, UpdatedAt: old})
	writeItem(t, dir, "features", &models.Node{ID: "feat-aaaa0002", Type: "feature", Title: "Stale wip", Status: models.StatusInProgress, CreatedAt: old, UpdatedAt: old})
	writeItem(t, dir, "bugs", &models.Node{ID: "bug-bbbb0001", Type: "bug", Title: "Done bug", Status: models.StatusDone, CreatedAt: old, UpdatedAt: mcpTestNow})

	store := claimledger.NewStore(dir)
	for i, root := range []string{"rootA", "rootB"} {
		_, _, err := store.Open(root, claimledger.Episode{
			ID: fmt.Sprintf("ep-%d", i), WorkItemID: "feat-aaaa0002", SessionID: root,
			RootSessionID: root, AgentID: "__root__", StartedAt: mcpTestNow.Add(-time.Hour),
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	writeShard(t, dir, "sess-one", []sigSpec{
		{canonical: "api_request", kind: "log", cost: fp(1.25), offset: -time.Hour},
		{canonical: "api_request", kind: "log", cost: fp(0.75), offset: -50 * time.Minute},
		{canonical: "api_request", kind: "span", cost: fp(99), offset: -50 * time.Minute}, // must not double count
		{canonical: "tool_result", kind: "log", tool: "Bash", toolUseID: "t1", success: bp(true), offset: -40 * time.Minute},
		{canonical: "tool_result", kind: "span", tool: "Bash", toolUseID: "t1", success: bp(true), offset: -40 * time.Minute},
		{canonical: "tool_result", kind: "log", tool: "Edit", toolUseID: "t2", success: bp(false), offset: -30 * time.Minute},
		{canonical: "tool_result", kind: "span", tool: "Edit", toolUseID: "t2", success: bp(false), offset: -30 * time.Minute},
		{canonical: "tool_blocked_on_user", kind: "span", tool: "Bash", toolUseID: "t3", durationMs: 90_000, offset: -20 * time.Minute},
		{canonical: "api_error", kind: "log", offset: -10 * time.Minute},
		{canonical: "compaction", kind: "log", offset: -5 * time.Minute},
	})
	writeShard(t, dir, "sess-two", []sigSpec{
		{canonical: "api_request", kind: "log", cost: fp(0.5), offset: -2 * time.Hour},
	})
	// Outside the window: must be ignored.
	writeShard(t, dir, "sess-old", []sigSpec{{canonical: "api_request", kind: "log", cost: fp(1000), offset: -200 * time.Hour}})
	old2 := mcpTestNow.Add(-200 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "sessions", "sess-old", "events.ndjson"), old2, old2); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestBuildOverview(t *testing.T) {
	dir := seedProject(t)
	res, err := buildOverview(dir, overviewInput{}, mcpTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.WorkItems.Total != 3 || res.WorkItems.ByStatus["blocked"] != 1 || res.WorkItems.ByType["bug"] != 1 {
		t.Errorf("work items = %+v", res.WorkItems)
	}
	if got := res.Cost.TotalUSD; got != 2.5 {
		t.Errorf("total cost = %v, want 2.5 (log api_request only, in-window only)", got)
	}
	if res.Cost.SessionsTotal != 2 || res.Cost.TopSessions[0].SessionID != "sess-one" {
		t.Errorf("sessions = %+v", res.Cost.TopSessions)
	}
	s := res.Cost.TopSessions[0]
	if s.ToolCalls != 2 || s.FailedToolCalls != 1 || s.PermissionWaits != 1 || s.PermissionWaitMs != 90_000 || s.APIErrors != 1 || s.Compactions != 1 {
		t.Errorf("sess-one aggregates = %+v", s)
	}
	kinds := map[string]attentionItem{}
	for _, a := range res.NeedsAttention {
		kinds[a.Kind+"/"+a.Ref] = a
	}
	for _, want := range []string{"blocked_work_item/feat-aaaa0001", "stale_in_progress/feat-aaaa0002",
		"claim_collision/feat-aaaa0002", "failed_tool_calls/sess-one", "permission_waits/sess-one", "api_errors/sess-one"} {
		if _, ok := kinds[want]; !ok {
			t.Errorf("missing attention item %s; have %v", want, kinds)
		}
	}
	if title := kinds["blocked_work_item/feat-aaaa0001"].Title; strings.ContainsAny(title, "\n") || len([]rune(title)) > maxTitleRunes {
		t.Errorf("title not sanitised: %q", title)
	}
	if res.NeedsAttention[0].Severity != "high" {
		t.Errorf("not sorted by severity: first = %+v", res.NeedsAttention[0])
	}
}

func TestBuildOverviewBounds(t *testing.T) {
	dir := seedProject(t)
	res, err := buildOverview(dir, overviewInput{MaxAttention: 2, MaxSessions: 1}, mcpTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NeedsAttention) != 2 || !res.AttentionTruncated || res.AttentionTotal <= 2 {
		t.Errorf("attention bounds: len=%d truncated=%v total=%d", len(res.NeedsAttention), res.AttentionTruncated, res.AttentionTotal)
	}
	if len(res.Cost.TopSessions) != 1 || res.Cost.TotalUSD != 2.5 {
		t.Errorf("cost bounds: %+v", res.Cost)
	}
}

func TestBuildOverviewNoTelemetry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := buildOverview(dir, overviewInput{}, mcpTestNow)
	if err != nil {
		t.Fatal(err)
	}
	if res.Telemetry.Available || res.Telemetry.Note == "" || res.NeedsAttention == nil || res.Cost.TopSessions == nil {
		t.Errorf("empty project result = %+v", res)
	}
}

// TestMCPServerEndToEnd drives the real MCP protocol (initialize, tools/list,
// tools/call) through an in-memory transport and checks the privacy and
// read-only contracts on the wire.
func TestMCPServerEndToEnd(t *testing.T) {
	dir := seedProject(t)
	before := listTree(t, dir)
	oldNow := mcpNow
	mcpNow = func() time.Time { return mcpTestNow }
	defer func() { mcpNow = oldNow }()

	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	srv, err := newMCPServer(dir).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	tools, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("no tools")
	}
	for _, tool := range tools.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("tool %s is not annotated read-only", tool.Name)
		}
		if tool.InputSchema == nil || tool.OutputSchema == nil {
			t.Errorf("tool %s lacks input/output schema", tool.Name)
		}
	}

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "wipnote_overview", Arguments: map[string]any{"since_hours": 24}})
	if err != nil || res.IsError {
		t.Fatalf("call: %v %+v", err, res)
	}
	raw, _ := json.Marshal(res)
	if strings.Contains(string(raw), secretPrompt) {
		t.Error("transcript/tool text leaked into a tool result")
	}
	if len(raw) > 64*1024 {
		t.Errorf("result %d bytes exceeds bound", len(raw))
	}
	var out overviewResult
	b, _ := json.Marshal(res.StructuredContent)
	if err := json.Unmarshal(b, &out); err != nil || out.SchemaVersion != overviewSchemaVersion || out.Cost.TotalUSD != 2.5 {
		t.Errorf("structured content = %s (%v)", b, err)
	}
	if after := listTree(t, dir); after != before {
		t.Errorf(".wipnote changed during read-only calls:\nbefore=%s\nafter=%s", before, after)
	}
}

func listTree(t *testing.T, root string) string {
	t.Helper()
	var sb strings.Builder
	_ = filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err == nil && !fi.IsDir() {
			fmt.Fprintf(&sb, "%s %d %d\n", p, fi.Size(), fi.ModTime().UnixNano())
		}
		return nil
	})
	return sb.String()
}
