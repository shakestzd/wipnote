package hooks

import (
	"database/sql"
	"os/exec"
	"strings"
	"testing"
)

// yoloCommitResult runs PreToolUse for a YOLO `git commit` against the
// read-only hook DB, whose agent_events table is always empty.
func yoloCommitResult(t *testing.T, repoDir, sessionID string) *HookResult {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", repoDir)
	t.Setenv("CLAUDE_PROJECT_DIR", repoDir)
	var database *sql.DB // hot hooks dispatch with no handle (feat-af8d1689)
	event := &CloudEvent{
		SessionID:      sessionID,
		CWD:            repoDir,
		ToolName:       "Bash",
		PermissionMode: "bypassPermissions",
		ToolInput:      map[string]any{"command": "git commit -m 'x'"},
	}
	res, err := PreToolUse(event, database)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	return res
}

func blockedBy(res *HookResult, needle string) bool {
	return res != nil && res.Decision == "block" && strings.Contains(res.Reason, needle)
}

// TestPreToolUse_YoloCommitAllowedAfterCanonicalTestRun: test-run and
// diff-review evidence comes from the session's tool-events.ndjson, not the
// empty ephemeral agent_events table.
func TestPreToolUse_YoloCommitAllowedAfterCanonicalTestRun(t *testing.T) {
	clearNestedEnv(t)
	repoDir := setupTempGitRepo(t)
	const sessionID = "canon-yolo-commit-1"
	co := exec.Command("git", "checkout", "-q", "-b", "yolo-feat-12345678")
	co.Dir = repoDir
	co.Env = cleanEnv()
	if out, err := co.CombinedOutput(); err != nil {
		t.Fatalf("git checkout: %v\n%s", err, out)
	}

	recordTool(t, repoDir, sessionID, "Read", "main.go", `{"file_path":"main.go"}`)
	// No test run recorded: still blocked.
	res := yoloCommitResult(t, repoDir, sessionID)
	if !blockedBy(res, "requires tests") {
		t.Fatalf("commit without a test run must be blocked, got %+v", res)
	}

	recordTool(t, repoDir, sessionID, "Bash", "go test ./...", `{"command":"go test ./..."}`)
	recordTool(t, repoDir, sessionID, "Bash", "git diff --stat", `{"command":"git diff --stat"}`)
	res = yoloCommitResult(t, repoDir, sessionID)
	if blockedBy(res, "requires tests") || blockedBy(res, "diff review") {
		t.Fatalf("commit after canonical test run + diff review was blocked: %+v", res)
	}
}
