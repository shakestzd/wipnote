package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/dbtest"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

func TestCheckOrchestratorResearchDelegationAdvisory_WarnsForRootWebBeforeDelegation(t *testing.T) {

	got := checkOrchestratorResearchDelegationAdvisory(
		&CloudEvent{ToolName: "web.search_query"},
		&toolUseContext{SessionID: "sess-orch-web"})
	if got == "" {
		t.Fatal("expected advisory for root-session web research before delegation")
	}
	if !strings.Contains(got, "delegate web/docs research") {
		t.Fatalf("expected delegation guidance, got %q", got)
	}
}

func TestCheckOrchestratorResearchDelegationAdvisory_SkipsForSubagent(t *testing.T) {

	got := checkOrchestratorResearchDelegationAdvisory(
		&CloudEvent{ToolName: "web.search_query"},
		&toolUseContext{SessionID: "sess-subagent-web", IsSubagent: true})
	if got != "" {
		t.Fatalf("expected no advisory for subagent research, got %q", got)
	}
}

// TestPreToolUse_NilDB_RunsDBIndependentGuards is the roborev-478 finding 1
// regression test: the read-only hot-hook dispatch may invoke PreToolUse with a
// nil DB (first run, deleted cache, DB not yet created, transient lock). The
// handler must NOT panic and must still run its DB-INDEPENDENT safety guards —
// in particular the .wipnote/-write block. Before the fix, a nil/unopenable DB
// caused the dispatch to skip the handler entirely, so direct .wipnote/ writes
// were silently allowed.
func TestPreToolUse_NilDB_RunsDBIndependentGuards(t *testing.T) {
	clearNestedEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_SESSION_ID", "sess-nildb-guard")

	// A structured Write targeting .wipnote/ must be BLOCKED even with a nil DB.
	writeEvent := &CloudEvent{
		ToolName: "Write",
		CWD:      projectDir,
		ToolInput: map[string]any{
			"file_path": filepath.Join(projectDir, ".wipnote", "features", "feat-x.html"),
			"content":   "<html>tamper</html>",
		},
	}
	result, err := PreToolUse(writeEvent) // nil DB: must not panic
	if err != nil {
		t.Fatalf("PreToolUse(nil DB) returned error: %v", err)
	}
	if result == nil || result.Decision != "block" {
		t.Fatalf("nil-DB PreToolUse must BLOCK a direct .wipnote/ Write; got %+v", result)
	}

	// A Bash command writing into .wipnote/ must also be blocked with a nil DB.
	bashEvent := &CloudEvent{
		ToolName:  "Bash",
		CWD:       projectDir,
		ToolInput: map[string]any{"command": "rm -f .wipnote/features/feat-x.html"},
	}
	bashResult, err := PreToolUse(bashEvent)
	if err != nil {
		t.Fatalf("PreToolUse(nil DB) returned error: %v", err)
	}
	if bashResult == nil || bashResult.Decision != "block" {
		t.Fatalf("nil-DB PreToolUse must BLOCK a Bash .wipnote/ write; got %+v", bashResult)
	}

	// A normal source-file Write with a nil DB must NOT panic. (It may still be
	// blocked by the DB-independent work-item guard when no work item is active —
	// that is correct guard behaviour; the point here is that the handler runs to
	// completion against a nil DB rather than panicking on a QueryRow.)
	okEvent := &CloudEvent{
		ToolName: "Read",
		CWD:      projectDir,
		ToolInput: map[string]any{
			"file_path": filepath.Join(projectDir, "main.go"),
		},
	}
	okResult, err := PreToolUse(okEvent) // Read is not a write tool — guards allow
	if err != nil {
		t.Fatalf("PreToolUse(nil DB) returned error: %v", err)
	}
	if okResult != nil && okResult.Decision == "block" {
		t.Fatalf("nil-DB PreToolUse wrongly blocked a Read: %q", okResult.Reason)
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

func TestResolveToolUseContext_DoesNotUseUnrelatedProjectClaim(t *testing.T) {
	tdb := setupTestDB(t)
	projectDir := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	if err := db.UpsertSession(tdb.DB, &models.Session{
		SessionID:     "sess-current-unclaimed",
		AgentAssigned: "codex",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	}); err != nil {
		t.Fatalf("UpsertSession current: %v", err)
	}
	if err := dbtest.SetSessionFamilyID(tdb.DB, "sess-current-unclaimed", "family-current"); err != nil {
		t.Fatalf("SetSessionFamilyID current: %v", err)
	}
	if err := db.UpsertSession(tdb.DB, &models.Session{
		SessionID:     "sess-unrelated-claim",
		AgentAssigned: "codex",
		Status:        "active",
		CreatedAt:     time.Now().UTC().Add(time.Second),
		ProjectDir:    projectDir,
	}); err != nil {
		t.Fatalf("UpsertSession unrelated: %v", err)
	}
	if err := dbtest.SetSessionFamilyID(tdb.DB, "sess-unrelated-claim", "family-other"); err != nil {
		t.Fatalf("SetSessionFamilyID unrelated: %v", err)
	}
	tdb.addFeature("bug-other-claim", "bug", "Other claimed bug", "in-progress")
	if err := dbtest.InsertClaim(tdb.DB, &models.Claim{
		ClaimID:          "clm-other-claim",
		WorkItemID:       "bug-other-claim",
		OwnerSessionID:   "sess-unrelated-claim",
		OwnerAgent:       "codex",
		ClaimedByAgentID: "codex",
		Status:           models.ClaimInProgress,
	}, 30*time.Minute); err != nil {
		t.Fatalf("ClaimItemOrRenew: %v", err)
	}

	ctx := resolveToolUseContext(&CloudEvent{
		AgentID:   "codex",
		SessionID: "sess-current-unclaimed",
		CWD:       projectDir,
		ToolName:  "apply_patch",
		ToolInput: map[string]any{"patch": "*** Begin Patch\n*** End Patch\n"},
	})
	if ctx == nil {
		t.Fatal("resolveToolUseContext returned nil")
	}
	if ctx.SessionID != "sess-current-unclaimed" {
		t.Fatalf("SessionID = %q, want current session", ctx.SessionID)
	}
	if ctx.FeatureID != "" || ctx.ClaimedItem != "" {
		t.Fatalf("FeatureID/ClaimedItem = %q/%q, want no unrelated claim", ctx.FeatureID, ctx.ClaimedItem)
	}
}

func TestCheckYoloWorkItemGuard_DiagnosticsIncludeCheckedSession(t *testing.T) {
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := checkYoloWorkItemGuard(
		"apply_patch", "", false, "codex-desktop-invocation", filepath.Join(projectDir, "main.go"), projectDir)
	if !strings.Contains(got, "Checked session: codex-desktop-invocation") {
		t.Fatalf("missing checked session diagnostic:\n%s", got)
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

// TestSplitShellCommandSegments_RespectsQuotes pins that a separator
// character inside a single- or double-quoted span is never a segment
// boundary — callers like isWipnoteCLICommand and bashCommandWritesWipnoteStore
// depend on this to avoid misclassifying quoted prose as a second command.
func TestSplitShellCommandSegments_RespectsQuotes(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want []string
	}{
		{"no quotes, splits normally", "a; b && c", []string{"a", "b", "c"}},
		{
			"semicolon inside double quotes is not a boundary",
			`printf '%s\n' "Never; rm .wipnote/x" > docs/rules.md`,
			[]string{`printf '%s\n' "Never; rm .wipnote/x" > docs/rules.md`},
		},
		{
			"pipe inside double quotes is not a boundary",
			`echo "a | b" > f`,
			[]string{`echo "a | b" > f`},
		},
		{
			"&& inside single quotes is not a boundary",
			`echo 'safe && unsafe' > f`,
			[]string{`echo 'safe && unsafe' > f`},
		},
		{
			"real separator after a closed quote still splits",
			`echo "a; b" && echo c`,
			[]string{`echo "a; b"`, `echo c`},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := splitShellCommandSegments(tc.cmd)
			if len(got) != len(tc.want) {
				t.Fatalf("splitShellCommandSegments(%q) = %q, want %q", tc.cmd, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("segment %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestIsBashwipnoteWrite_DoesNotBypassOnMention pins the store guard's
// target-based decision (GH-#180): a direct write is blocked even when the
// command mentions "wipnote", `git add .wipnote/` is blocked, and a heredoc or
// quoted mention of the store path written elsewhere is allowed.
func TestIsBashwipnoteWrite_DoesNotBypassOnMention(t *testing.T) {
	tests := []struct {
		name string
		tool string
		cmd  string
		want bool
	}{
		{"direct write mentioning wipnote", "Bash", "echo wipnote > .wipnote/features/feat-abc.html", true},
		{"git add store then commit", "Bash", `git add .wipnote/ && git commit -m "x"`, true},
		{"codex exec_command rm", "exec_command", "rm -f .wipnote/features/feat-abc.html", true},
		{"heredoc mention written elsewhere", "Bash", "cat > ~/some/other/notes.md <<'TXT'\nNever run git add against .wipnote/ by hand.\nTXT", false},
		{"quoted mention", "Bash", `echo "do not rm -rf .wipnote/ manually" >> docs/rules.md`, false},
		{"wipnote CLI", "Bash", "wipnote feature complete feat-abc", false},
		{"non-shell tool", "Read", "rm -rf .wipnote/", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			event := &CloudEvent{ToolName: tc.tool, ToolInput: map[string]any{"command": tc.cmd}}
			if got := isBashwipnoteWrite(event); got != tc.want {
				t.Errorf("isBashwipnoteWrite(%s %q) = %v, want %v", tc.tool, tc.cmd, got, tc.want)
			}
		})
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
