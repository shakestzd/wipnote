package hooks

import (
	"fmt"
	"strings"
	"testing"
)

// A worker's AskUserQuestion keeps each question's options, header and
// multiSelect on the question event, so a filed record can be answered; the
// tool_end carries the answer the person picked in the pane.
func TestDelegateEvents_AskKeepsOptionsAndPaneAnswer(t *testing.T) {
	project, runDir, _ := delegateWorker(t)
	input := map[string]any{"questions": []any{
		map[string]any{"question": "Which file?", "header": "File", "multiSelect": false, "options": []any{
			map[string]any{"label": "a.go", "description": "small"}, map[string]any{"label": "b.go"}}},
		map[string]any{"question": "Which tests?", "multiSelect": true, "options": []any{
			map[string]any{"label": "unit"}, map[string]any{"label": "e2e"}}},
	}}
	recordDelegateToolStart(&CloudEvent{ToolName: "AskUserQuestion", ToolUseID: "tu-ask", ToolInput: input}, project, "sid-1")
	end := &CloudEvent{ToolName: "AskUserQuestion", ToolUseID: "tu-ask", ToolInput: input, ToolResult: map[string]any{
		"answers": map[string]any{"Which file?": "b.go", "Which tests?": []any{"unit", "e2e"}},
	}}
	recordDelegateToolEnd(end, project, "sid-1", false)
	recordDelegateToolEnd(&CloudEvent{ToolName: "AskUserQuestion", ToolUseID: "tu-x", ToolInput: input}, project, "sid-1", true)

	lines := eventsOf(t, runDir)
	if len(lines) != 3 {
		t.Fatalf("%v", lines)
	}
	asks := fmt.Sprint(lines[0]["asks"])
	for _, want := range []string{"Which file?", "File", "a.go", "b.go", "Which tests?", "multi_select:true"} {
		if !strings.Contains(asks, want) {
			t.Fatalf("question event asks %s lacks %q", asks, want)
		}
	}
	got := fmt.Sprint(lines[1]["asks"])
	if !strings.Contains(got, "answer:b.go") || !strings.Contains(got, "answer:unit, e2e") {
		t.Fatalf("tool_end asks %s lack the pane answers", got)
	}
	if lines[2]["summary"] != "failed" || lines[2]["asks"] != nil {
		t.Fatalf("a failed ask is not answered: %v", lines[2])
	}
}
