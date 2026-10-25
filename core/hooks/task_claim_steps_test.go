package hooks

import (
	"os"
	"path/filepath"
	"testing"
)

// taskClaimProject is a temp wipnote project for the task hooks. With claim
// set, the session holds an open claim on feat-abcd1234 in the canonical
// claim ledger, the way `wipnote feature start` records it.
func taskClaimProject(t *testing.T, sessionID string, claim bool) string {
	t.Helper()
	unsetenv(t, "WIPNOTE_PROJECT_DIR")
	unsetenv(t, "CLAUDE_PROJECT_DIR")
	unsetenv(t, "WIPNOTE_TASK_STEPS")
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if claim {
		openCanonicalClaim(t, wipnoteDir, sessionID, sessionID, "claude-code", "feat-abcd1234")
	}
	return projectDir
}

// stubTaskCompletionGate records whether the TaskCompleted quality gate ran.
func stubTaskCompletionGate(t *testing.T) *int {
	t.Helper()
	calls := 0
	prev := runTaskCompletionGateFn
	runTaskCompletionGateFn = func(string) taskCompletionGateResult {
		calls++
		return taskCompletionGateResult{Passed: true}
	}
	t.Cleanup(func() { runTaskCompletionGateFn = prev })
	return &calls
}

// TestTaskHooks_MirrorStepsFromTheClaimLedger pins the task-step rewire
// (feat-174dc958): the active work item comes from the canonical claim
// ledger, not the hook database. With an open claim, a task naming the work
// item creates and completes its step; with no claim, a task naming no work
// item, or mirroring turned off, neither happens. The
// quality gate stays off either way: it runs the project's test command
// synchronously inside the hook and is held until the owner decides.
func TestTaskHooks_MirrorStepsFromTheClaimLedger(t *testing.T) {
	for _, tc := range []struct {
		name, subject, steps string
		claim                bool
		want                 int
	}{
		{"open claim", "Wire the hooks for feat-abcd1234", "", true, 1},
		{"no claim", "Wire the hooks for feat-abcd1234", "", false, 0},
		{"claim, task names no work item", "Draft the plan YAML", "", true, 0},
		{"claim, step mirroring off", "Wire the hooks for feat-abcd1234", "off", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const sid = "aaaa3333-bbbb-cccc-dddd-eeee00005555"
			projectDir := taskClaimProject(t, sid, tc.claim)
			t.Setenv("WIPNOTE_TASK_STEPS", tc.steps)
			addCalls, completeCalls := stubTaskStepFns(t)
			gateCalls := stubTaskCompletionGate(t)
			ev := &CloudEvent{SessionID: sid, CWD: projectDir, TaskID: "t-claim", TaskSubject: tc.subject}
			if _, err := TaskCreated(ev, nil); err != nil {
				t.Fatalf("TaskCreated: %v", err)
			}
			if _, err := TaskCompleted(ev, nil); err != nil {
				t.Fatalf("TaskCompleted: %v", err)
			}
			if *addCalls != tc.want || *completeCalls != tc.want {
				t.Errorf("add=%d complete=%d, want %d each", *addCalls, *completeCalls, tc.want)
			}
			if *gateCalls != 0 {
				t.Errorf("quality gate ran %d time(s); it must stay off", *gateCalls)
			}
		})
	}
}

// TestTaskFields_TypedFieldWinsOverTaskData pins where a task's subject and
// description come from: the typed field first, then task_data.
func TestTaskFields_TypedFieldWinsOverTaskData(t *testing.T) {
	data := map[string]any{"subject": "from data", "description": "data desc"}
	if s, d := taskFields(&CloudEvent{TaskSubject: "typed", TaskData: data}); s != "typed" || d != "data desc" {
		t.Errorf("typed subject: got (%q, %q)", s, d)
	}
	if s, _ := taskFields(&CloudEvent{TaskData: data}); s != "from data" {
		t.Errorf("task_data fallback: got %q", s)
	}
}
