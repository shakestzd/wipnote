package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mcpToolSpec is one read-only tool. The same run closure backs the MCP
// handler and `wipnote report`, so both surfaces share one code path.
type mcpToolSpec struct {
	Name     string
	register func(s *mcp.Server, wipnoteDir string)
	run      func(wipnoteDir string, args json.RawMessage, now time.Time) (any, error)
}

func newToolSpec[In, Out any](name, desc string, fn func(dir string, in In, now time.Time) (Out, error)) mcpToolSpec {
	return mcpToolSpec{
		Name: name,
		register: func(s *mcp.Server, dir string) {
			mcp.AddTool(s, &mcp.Tool{
				Name:        name,
				Description: desc,
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
			}, func(_ context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
				out, err := fn(dir, in, mcpNow())
				return nil, out, err
			})
		},
		run: func(dir string, args json.RawMessage, now time.Time) (any, error) {
			var in In
			if len(bytes.TrimSpace(args)) > 0 {
				dec := json.NewDecoder(bytes.NewReader(args))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&in); err != nil {
					return nil, fmt.Errorf("invalid arguments for %s: %w", name, err)
				}
			}
			return fn(dir, in, now)
		},
	}
}

// mcpToolSpecs lists every tool the server exposes. There are deliberately no
// write tools.
func mcpToolSpecs() []mcpToolSpec {
	return []mcpToolSpec{
		newToolSpec("wipnote_overview", "One-call project dashboard: work-item counts by status/type, a bounded "+
			"needs-attention list (blocked or stale work items, claim collisions, failed tool "+
			"calls, permission waits, API errors) and cost per session from OpenTelemetry "+
			"(single source of truth for cost). Returns ids, counts, durations and USD only.", buildOverview),
		newToolSpec("wipnote_sessions", "Sessions active in a window with cost, tokens, tool failures, permission "+
			"waits, API errors, compactions and subagent counts. sort: cost (default), recent or failures. "+
			"Returns ids, counts, durations and USD only.", buildSessions),
		newToolSpec("wipnote_session_trace", "Drill into one session (by wipnote session id): slowest tools, "+
			"compaction timestamps, subagent summary by agent type and cost by model. Returns names, counts, "+
			"durations and USD only; never prompt text or tool input/output.", buildSessionTrace),
		newToolSpec("wipnote_work_items", "Work items (features, bugs, spikes) with status, priority, age, open "+
			"claim holders and cost attributed from claim-episode intervals. Filter by status, type, stale_days. "+
			"Titles are untrusted text.", buildWorkItems),
		newToolSpec("wipnote_cost", "Cost buckets grouped by session, model, day (UTC) or work_item from OTel "+
			"api_request signals. Returns USD, token counts and unpriced request counts only.", buildCost),
	}
}

// runMCPTool resolves a tool by name (the wipnote_ prefix is optional) and
// runs it, returning the structured result.
func runMCPTool(wipnoteDir, name string, args json.RawMessage, now time.Time) (any, error) {
	full := name
	if !strings.HasPrefix(full, "wipnote_") {
		full = "wipnote_" + full
	}
	var names []string
	for _, t := range mcpToolSpecs() {
		if t.Name == full {
			return t.run(wipnoteDir, args, now)
		}
		names = append(names, strings.TrimPrefix(t.Name, "wipnote_"))
	}
	sort.Strings(names)
	return nil, fmt.Errorf("unknown tool %q (available: %s)", name, strings.Join(names, ", "))
}
