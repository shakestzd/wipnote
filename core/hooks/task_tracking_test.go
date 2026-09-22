package hooks

import "testing"

// TestBuildStepDesc_* tests were removed: they re-implemented addTaskStep's
// string-building logic inline rather than calling the function, providing
// zero refactor protection. The same behavior is covered by
// TestTeammateIdle_RecordsTeammateName and the TaskCreated/TaskCompleted
// tests in missing_events_test.go.

// TestTaskNamesWorkItem covers the GH-#169 (bug-664276df) id-detection gate:
// a task is attributable only when its subject or description names a
// wipnote work-item ID.
func TestTaskNamesWorkItem(t *testing.T) {
	tests := []struct {
		name        string
		subject     string
		description string
		want        bool
	}{
		{"id in subject", "Fix bug-9972133d redirect regex", "", true},
		{"id in description", "Implement the fix", "Refs feat-abc12345 for context", true},
		{"parenthesized id still matches", "Ship it (spk-deadbeef)", "", true},
		{"unrelated orchestrator subject", "Run mandatory v4 research and ground all file paths", "", false},
		{"draft plan subject", "Draft the blocks-first plan YAML", "", false},
		{"validate plan subject", "Validate the plan and run critique", "", false},
		{"empty subject and description", "", "", false},
		{"short hex is not 8 chars", "Fix bug-abc123", "", false},
		{"unknown prefix is not a work item", "Fix task-9972133d", "", false},
		{"uppercase hex does not match", "Fix bug-9972133D", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := taskNamesWorkItem(tc.subject, tc.description); got != tc.want {
				t.Errorf("taskNamesWorkItem(%q, %q) = %v, want %v", tc.subject, tc.description, got, tc.want)
			}
		})
	}
}

// TestTaskStepsDisabled covers the WIPNOTE_TASK_STEPS=off opt-out.
func TestTaskStepsDisabled(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"unset", "", false},
		{"off", "off", true},
		{"case insensitive", "OFF", true},
		{"padded with whitespace", "  off  ", true},
		{"on is not disabled", "on", false},
		{"garbage is not disabled", "true", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("WIPNOTE_TASK_STEPS", tc.env)
			if got := taskStepsDisabled(); got != tc.want {
				t.Errorf("taskStepsDisabled() with WIPNOTE_TASK_STEPS=%q = %v, want %v", tc.env, got, tc.want)
			}
		})
	}
}
