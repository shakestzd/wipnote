package hooks

import (
	"os"
	"testing"
)

// readInputFrom runs ReadInputRaw against payload as stdin.
func readInputFrom(t *testing.T, payload string) *CloudEvent {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(payload); err != nil {
		t.Fatal(err)
	}
	w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig; r.Close() })
	ev, _, err := ReadInputRaw()
	if err != nil {
		t.Fatalf("ReadInputRaw: %v", err)
	}
	return ev
}

// TestReadInputRaw_ClaudeToolResponse uses PostToolUse payloads captured live
// from Claude Code 2.1.302. Claude sends "tool_response", never
// "tool_result", so before the fix ToolResult was always nil on Claude and
// `git commit` output never reached commit-message parsing.
func TestReadInputRaw_ClaudeToolResponse(t *testing.T) {
	bash := readInputFrom(t, `{"hook_event_name":"PostToolUse","tool_name":"Bash",
		"tool_use_id":"toolu_1","duration_ms":12,
		"tool_response":{"stdout":"[main abc1234] feat: x","stderr":"","interrupted":false,"isImage":false}}`)
	if got := summarizeToolOutput(bash.ToolResult); got != "[main abc1234] feat: x" {
		t.Errorf("Bash summary = %q, want the stdout text", got)
	}
	if bash.DurationMS != 12 {
		t.Errorf("DurationMS = %v, want 12", bash.DurationMS)
	}

	write := readInputFrom(t, `{"tool_name":"Write","tool_response":{"type":"create","filePath":"/x"}}`)
	if write.ToolResult["filePath"] != "/x" {
		t.Errorf("Write ToolResult = %v, want the tool_response object", write.ToolResult)
	}

	// An MCP tool may return an array; it must not make the payload unparseable.
	mcp := readInputFrom(t, `{"tool_name":"mcp__x__y","tool_response":[{"type":"text","text":"ok"}]}`)
	if mcp.ToolResult == nil || mcp.ToolResult["output"] == "" {
		t.Errorf("array tool_response should be kept as output, got %v", mcp.ToolResult)
	}

	// tool_result, if a build ever sends it, still wins.
	legacy := readInputFrom(t, `{"tool_name":"Bash","tool_result":{"output":"legacy"},"tool_response":{"stdout":"new"}}`)
	if got := summarizeToolOutput(legacy.ToolResult); got != "legacy" {
		t.Errorf("summary = %q, want tool_result to take precedence", got)
	}
}

// TestParseEventForHarness_ClaudeToolResponse covers the path the hook CLI
// actually uses (runHookNamed -> ParseEventForHarness). The first fix only
// touched ReadInputRaw, which has no production caller (Codex review, #208).
func TestParseEventForHarness_ClaudeToolResponse(t *testing.T) {
	ev, err := ParseEventForHarness(HarnessClaude, []byte(`{"hook_event_name":"PostToolUse","tool_name":"Bash",
		"tool_response":{"stdout":"[main abc1234] feat: x","stderr":""}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := summarizeToolOutput(ev.ToolResult); got != "[main abc1234] feat: x" {
		t.Errorf("summary = %q, want the stdout text", got)
	}
}
