package hooks

import (
	"reflect"
	"strings"
	"testing"
)

// TestPreToolUse_GuardOrder pins the PreToolUse guard pipeline: the guards run
// in this order (see TestPreToolUse_GuardOrderDecisions for the decision half).
func TestPreToolUse_GuardOrder(t *testing.T) {
	want := []string{
		"wipnote-write",
		"bash-cwd",
		"project-divergence",
		"plan-mode-bypass",
		"orchestrator-research-advisory",
		"orchestrator-strict",
		"subagent-work-item",
		"subagent-commit",
		"port-drift-commit",
		"dependency-research-commit",
		"research-dispatch",
		"yolo-work-item",
		"read-before-write",
		"external-tech-research",
		"yolo-bash-research",
		"yolo-worktree",
		"yolo-session",
		"file-overlap",
	}
	var got []string
	for _, g := range preToolUseGuards() {
		got = append(got, g.name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("guard order changed:\n got  %v\n want %v", got, want)
	}
}

// TestPreToolUse_GuardOrderDecisions is the decision half of the guard-order
// pin: a fixed set of tool calls gets the same decision it got before the
// pipeline was split out of one function (the same cases pass against the
// pre-split PreToolUse).
func TestPreToolUse_GuardOrderDecisions(t *testing.T) {
	cases := []guardOrderCase{
		{"bash write to .wipnote blocks", "Bash", map[string]any{"command": "rm .wipnote/features/feat-x.html"}, "", true, "managed by wipnote CLI"},
		{"bare cd blocks", "Bash", map[string]any{"command": "cd packages/go && go build ./..."}, "", true, "cd"},
		{"plan mode bash write to .wipnote still blocks", "Bash", map[string]any{"command": "rm .wipnote/features/feat-x.html"}, "plan", true, "managed by wipnote CLI"},
		{"plan mode bare cd still blocks", "Bash", map[string]any{"command": "cd packages/go && go build ./..."}, "plan", true, "cd"},
		{"write without a work item blocks", "Write", map[string]any{"file_path": "main.go"}, "", true, "active work item is required"},
		{"plan mode write without a work item allows", "Write", map[string]any{"file_path": "main.go"}, "plan", false, ""},
		{"plan mode read allows", "Read", map[string]any{"file_path": "README.md"}, "plan", false, ""},
		{"read allows", "Read", map[string]any{"file_path": "README.md"}, "", false, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { runGuardOrderCase(t, tc) })
	}
}

// guardOrderCase is one fixed tool call and the decision PreToolUse returns.
type guardOrderCase struct {
	name       string
	tool       string
	input      map[string]any
	permission string
	wantBlock  bool
	wantReason string
}

// runGuardOrderCase runs tc through PreToolUse against a fresh test DB.
func runGuardOrderCase(t *testing.T, tc guardOrderCase) {
	t.Helper()
	tdb := setupTestDB(t)
	t.Setenv("WIPNOTE_SESSION_ID", "test-sess")
	event := &CloudEvent{
		AgentID:        "claude-code",
		SessionID:      "test-sess",
		CWD:            t.TempDir(),
		ToolName:       tc.tool,
		ToolInput:      tc.input,
		PermissionMode: tc.permission,
		ToolUseID:      "guard-order-" + tc.tool,
	}
	result, err := PreToolUse(event, tdb.DB)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result == nil {
		t.Fatal("PreToolUse returned nil result")
	}
	blocked := result.Decision == "block"
	if blocked != tc.wantBlock {
		t.Fatalf("blocked=%v, want %v (reason %q)", blocked, tc.wantBlock, result.Reason)
	}
	if tc.wantReason != "" && !strings.Contains(result.Reason, tc.wantReason) {
		t.Errorf("reason %q does not contain %q", result.Reason, tc.wantReason)
	}
}
