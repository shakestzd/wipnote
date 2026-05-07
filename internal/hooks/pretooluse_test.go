package hooks

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/models"
)

// makeSessionDB creates an in-memory DB with a session row pointing to projectDir.
func makeSessionDB(t *testing.T, sessionID, projectDir string) *sql.DB {
	t.Helper()
	database, err := db.Open(filepath.Join(t.TempDir(), "wipnote.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	s := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	}
	if err := db.InsertSession(database, s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}
	return database
}

func TestCheckProjectDivergence_SameProject(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Isolate from the developer's real environment: the hint file and env vars
	// must not redirect ResolveProjectDir to the real wipnote project dir.
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	sessionID := "sess-same-project"
	database := makeSessionDB(t, sessionID, projectDir)

	event := &CloudEvent{
		ToolName: "Write",
		CWD:      projectDir,
		ToolInput: map[string]any{
			"path":    filepath.Join(projectDir, "foo.go"),
			"content": "package main",
		},
	}

	result := checkProjectDivergence(event, database, sessionID)
	if result != nil {
		t.Errorf("expected nil (allow) for same project, got: %+v", result)
	}
}

func TestCheckProjectDivergence_DifferentProject_WriteTool_Blocks(t *testing.T) {
	sessionProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sessionProject, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir session project: %v", err)
	}
	otherProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(otherProject, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir other project: %v", err)
	}

	sessionID := "sess-diff-project-write"
	database := makeSessionDB(t, sessionID, sessionProject)

	for _, toolName := range []string{"Write", "Edit", "MultiEdit", "Bash", "NotebookEdit", "Agent"} {
		t.Run(toolName, func(t *testing.T) {
			// Unset env var so ResolveProjectDir uses CWD, not env override.
			t.Setenv("CLAUDE_PROJECT_DIR", "")

			event := &CloudEvent{
				ToolName:  toolName,
				CWD:       otherProject,
				ToolInput: map[string]any{"path": filepath.Join(otherProject, "foo.go")},
			}

			result := checkProjectDivergence(event, database, sessionID)
			if result == nil {
				t.Fatalf("expected block for %s across projects, got nil", toolName)
			}
			if result.Decision != "block" {
				t.Errorf("expected decision=block, got %q", result.Decision)
			}
			if !strings.Contains(result.Reason, "different project") {
				t.Errorf("expected 'different project' in reason, got: %q", result.Reason)
			}
		})
	}
}

func TestCheckProjectDivergence_DifferentProject_ReadTool_Allows(t *testing.T) {
	sessionProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sessionProject, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir session project: %v", err)
	}
	otherProject := t.TempDir()
	if err := os.MkdirAll(filepath.Join(otherProject, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir other project: %v", err)
	}

	sessionID := "sess-diff-project-read"
	database := makeSessionDB(t, sessionID, sessionProject)

	for _, toolName := range []string{"Read", "Grep", "Glob", "WebSearch", "WebFetch"} {
		t.Run(toolName, func(t *testing.T) {
			t.Setenv("CLAUDE_PROJECT_DIR", "")

			event := &CloudEvent{
				ToolName:  toolName,
				CWD:       otherProject,
				ToolInput: map[string]any{"path": filepath.Join(otherProject, "foo.go")},
			}

			result := checkProjectDivergence(event, database, sessionID)
			if result != nil {
				t.Errorf("expected nil (allow) for read-only %s across projects, got: %+v", toolName, result)
			}
		})
	}
}

func TestCheckProjectDivergence_NoSessionProjectDir_Allows(t *testing.T) {
	// Session has no project_dir stored — should not block anything.
	database, err := db.Open(filepath.Join(t.TempDir(), "wipnote.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	defer database.Close()

	sessionID := "sess-no-project-dir"
	s := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    "", // empty — no project_dir stored
	}
	if err := db.InsertSession(database, s); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	event := &CloudEvent{
		ToolName:  "Write",
		CWD:       t.TempDir(),
		ToolInput: map[string]any{"path": "/some/other/path/foo.go"},
	}

	result := checkProjectDivergence(event, database, sessionID)
	if result != nil {
		t.Errorf("expected nil (allow) when session has no project_dir, got: %+v", result)
	}
}

func TestCheckProjectDivergence_EmptySessionID_Allows(t *testing.T) {
	event := &CloudEvent{
		ToolName:  "Write",
		CWD:       t.TempDir(),
		ToolInput: map[string]any{"path": "/foo.go"},
	}
	database, _ := db.Open(filepath.Join(t.TempDir(), "wipnote.db"))
	defer database.Close()

	result := checkProjectDivergence(event, database, "")
	if result != nil {
		t.Errorf("expected nil (allow) for empty sessionID, got: %+v", result)
	}
}

func TestIsWriteTool(t *testing.T) {
	writeTools := []string{"Write", "Edit", "MultiEdit", "apply_patch", "Bash", "exec_command", "functions.exec_command", "NotebookEdit", "Agent"}
	for _, name := range writeTools {
		if !isWriteTool(name) {
			t.Errorf("expected %s to be a write tool", name)
		}
	}
	readTools := []string{"Read", "Grep", "Glob", "WebSearch", "WebFetch", "ToolSearch", "TaskList", "TaskGet", "AskUserQuestion"}
	for _, name := range readTools {
		if isWriteTool(name) {
			t.Errorf("expected %s to NOT be a write tool", name)
		}
	}
}

func TestCheckSubagentWorkItemGuard(t *testing.T) {
	tests := []struct {
		name        string
		tool        string
		isSubagent  bool
		hasWorkItem bool
		blocked     bool
	}{
		{"subagent Write no work item blocks", "Write", true, false, true},
		{"subagent Edit no work item blocks", "Edit", true, false, true},
		{"subagent MultiEdit no work item blocks", "MultiEdit", true, false, true},
		{"subagent apply_patch no work item blocks", "apply_patch", true, false, true},
		{"subagent Write with work item allows", "Write", true, true, false},
		{"subagent Edit with work item allows", "Edit", true, true, false},
		{"subagent apply_patch with work item allows", "apply_patch", true, true, false},
		{"subagent Bash no work item allows (not a write-only guard)", "Bash", true, false, false},
		{"subagent Read no work item allows", "Read", true, false, false},
		{"non-subagent Write no work item allows", "Write", false, false, false},
		{"non-subagent Edit no work item allows", "Edit", false, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkSubagentWorkItemGuard(tt.tool, tt.isSubagent, tt.hasWorkItem, "sess-abc123", true, "feat-xyz", "feat-xyz")
			if tt.blocked && result == "" {
				t.Errorf("expected block for tool=%s isSubagent=%v hasWorkItem=%v, got allow",
					tt.tool, tt.isSubagent, tt.hasWorkItem)
			}
			if !tt.blocked && result != "" {
				t.Errorf("expected allow for tool=%s isSubagent=%v hasWorkItem=%v, got block: %s",
					tt.tool, tt.isSubagent, tt.hasWorkItem, result)
			}
		})
	}
}

func TestCheckSubagentWorkItemGuard_DiagnosticFields(t *testing.T) {
	tests := []struct {
		name        string
		sessionID   string
		isYoloMode  bool
		isSubagent  bool
		featureID   string
		claimedItem string
		wantSession string // expected session prefix in message
		wantFeature string
		wantClaim   string
	}{
		{
			name:        "all fields populated",
			sessionID:   "abcdef1234567890",
			isYoloMode:  true,
			isSubagent:  true,
			featureID:   "feat-aabbccdd",
			claimedItem: "",
			wantSession: "abcdef12",
			wantFeature: "feat-aabbccdd",
			wantClaim:   "none",
		},
		{
			name:        "no feature no claim",
			sessionID:   "short",
			isYoloMode:  false,
			isSubagent:  true,
			featureID:   "",
			claimedItem: "",
			wantSession: "short",
			wantFeature: "none",
			wantClaim:   "none",
		},
		{
			name:        "claim present no feature",
			sessionID:   "sess-00001111",
			isYoloMode:  true,
			isSubagent:  true,
			featureID:   "",
			claimedItem: "bug-99887766",
			wantSession: "sess-000",
			wantFeature: "none",
			wantClaim:   "bug-99887766",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkSubagentWorkItemGuard("Write", tt.isSubagent, false, tt.sessionID, tt.isYoloMode, tt.featureID, tt.claimedItem)
			if result == "" {
				t.Fatal("expected block message, got empty string")
			}
			for _, field := range []string{tt.wantSession, tt.wantFeature, tt.wantClaim} {
				if !strings.Contains(result, field) {
					t.Errorf("block message missing %q\nfull message:\n%s", field, result)
				}
			}
			// Verify yolo and subagent booleans appear.
			yoloStr := "yolo=false"
			if tt.isYoloMode {
				yoloStr = "yolo=true"
			}
			subagentStr := "subagent=false"
			if tt.isSubagent {
				subagentStr = "subagent=true"
			}
			if !strings.Contains(result, yoloStr) {
				t.Errorf("block message missing %q\nfull message:\n%s", yoloStr, result)
			}
			if !strings.Contains(result, subagentStr) {
				t.Errorf("block message missing %q\nfull message:\n%s", subagentStr, result)
			}
		})
	}
}

func TestPreToolUseRecordsClaimedWorkItemFeatureID(t *testing.T) {
	tdb := setupTestDB(t)

	tdb.addFeature("bug-claim-001", "bug", "Claimed bug", "in-progress")
	claim := &models.Claim{
		ClaimID:          "clm-claim-001",
		WorkItemID:       "bug-claim-001",
		OwnerSessionID:   "test-sess",
		OwnerAgent:       "codex",
		ClaimedByAgentID: "codex",
		Status:           models.ClaimInProgress,
	}
	if err := db.ClaimItemOrRenew(tdb.DB, claim, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItemOrRenew: %v", err)
	}

	event := &CloudEvent{
		AgentID:   "codex",
		SessionID: "test-sess",
		CWD:       t.TempDir(),
		ToolName:  "Read",
		ToolInput: map[string]any{"file_path": "internal/hooks/pretooluse.go"},
		ToolUseID: "claimed-feature-validation",
	}
	result, err := PreToolUse(event, tdb.DB)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if result.Decision == "block" {
		t.Fatalf("Read should not be blocked: %s", result.Reason)
	}

	var featureID string
	if err := tdb.DB.QueryRow(
		`SELECT COALESCE(feature_id, '') FROM agent_events WHERE step_id = ?`,
		"claimed-feature-validation",
	).Scan(&featureID); err != nil {
		t.Fatalf("query event feature_id: %v", err)
	}
	if featureID != "bug-claim-001" {
		t.Fatalf("feature_id = %q, want bug-claim-001", featureID)
	}
}

// TestIsBashFileWrite_NewPatterns verifies the write-intent patterns added in
// the roborev-job-14 fix: explicit fd-1 redirect (1>), combined redirects (&>, &>>),
// find -delete, and the negative case cat ... 2>/dev/null (must NOT block).
func TestIsBashFileWrite_NewPatterns(t *testing.T) {
	mustBlock := []struct {
		name string
		cmd  string
	}{
		{"explicit fd 1 redirect", "echo x 1>file.txt"},
		{"explicit fd 1 append", "echo x 1>>file.txt"},
		{"combined redirect &>", "echo x &>file.txt"},
		{"combined redirect &>>", "echo x &>>file.txt"},
		{"find -delete", "find . -name '*.tmp' -delete"},
		{"find -delete with other flags", "find /tmp -mtime +7 -delete"},
	}
	for _, tc := range mustBlock {
		t.Run(tc.name, func(t *testing.T) {
			event := &CloudEvent{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": tc.cmd},
			}
			if !isBashFileWrite(event) {
				t.Errorf("isBashFileWrite should be true for write-intent command %q", tc.cmd)
			}
		})
	}

	mustAllow := []struct {
		name string
		cmd  string
	}{
		// Benign stderr-only redirect to /dev/null: must not be classified as write.
		{"cat with stderr devnull", "cat foo 2>/dev/null"},
		// fd-to-fd redirect: does not write to a file.
		{"fd-to-fd redirect", "cmd >&2"},
	}
	for _, tc := range mustAllow {
		t.Run(tc.name, func(t *testing.T) {
			event := &CloudEvent{
				ToolName:  "Bash",
				ToolInput: map[string]any{"command": tc.cmd},
			}
			if isBashFileWrite(event) {
				t.Errorf("isBashFileWrite should be false for command %q", tc.cmd)
			}
		})
	}
}

func TestIsWipnoteCLICommandAnchorsExecutable(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"bare wipnote", "wipnote feature start feat-abc", true},
		{"env-prefixed wipnote", "WIPNOTE_AGENT_ID=codex wipnote feature start feat-abc >/dev/null 2>&1", true},
		{"compound wipnote only", "wipnote feature start feat-abc && wipnote status", true},
		{"path wipnote", "/usr/local/bin/wipnote status", true},
		{"mentioned only", "echo wipnote > .wipnote/features/feat-abc.html", false},
		{"mixed direct write", "wipnote status && echo x > .wipnote/features/feat-abc.html", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWipnoteCLICommand(tc.cmd); got != tc.want {
				t.Fatalf("isWipnoteCLICommand(%q) = %v, want %v", tc.cmd, got, tc.want)
			}
		})
	}
}

func TestIsBashwipnoteWrite_DoesNotBypassOnMention(t *testing.T) {
	event := &CloudEvent{
		ToolName:  "Bash",
		ToolInput: map[string]any{"command": "echo wipnote > .wipnote/features/feat-abc.html"},
	}
	if !isBashwipnoteWrite(event) {
		t.Fatal("direct .wipnote write that merely mentions wipnote should be blocked")
	}
}

func TestCheckBashCwdGuard(t *testing.T) {
	tests := []struct {
		name    string
		tool    string
		cmd     string
		blocked bool
	}{
		{"bare cd blocks", "Bash", "cd packages/go && go build ./...", true},
		{"bare cd with spaces blocks", "Bash", "cd  packages/go && go test", true},
		{"subshell allowed", "Bash", "(cd packages/go && go build ./...)", false},
		{"no cd allowed", "Bash", "go build ./...", false},
		{"absolute path cd blocks", "Bash", "cd /tmp/foo && ls", true},
		{"cd alone no &&", "Bash", "cd packages/go", false},
		{"non-Bash tool ignored", "Read", "cd packages/go && cat file", false},
		{"empty command allowed", "Bash", "", false},
		{"semicolon cd allowed", "Bash", "echo hi; cd foo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := &CloudEvent{
				ToolName:  tt.tool,
				ToolInput: map[string]any{"command": tt.cmd},
			}
			result := checkBashCwdGuard(event)
			if tt.blocked && result == "" {
				t.Errorf("expected block for %q, got allow", tt.cmd)
			}
			if !tt.blocked && result != "" {
				t.Errorf("expected allow for %q, got block: %s", tt.cmd, result)
			}
		})
	}
}
