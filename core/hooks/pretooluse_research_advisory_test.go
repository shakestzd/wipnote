package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// TestPreToolUse_ResearchGuardsAdviseOutsideYolo pins the research-guard
// posture: a dependency-manifest edit with no web research this session is
// blocked in YOLO mode, where no person reviews the work, and only advised in
// an attended session.
func TestPreToolUse_ResearchGuardsAdviseOutsideYolo(t *testing.T) {
	clearNestedEnv(t)
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")
	t.Setenv("WIPNOTE_YOLO", "")
	stubRouteSQLAsync(t, true)

	cases := []struct {
		name       string
		permission string
		wantBlock  bool
	}{
		{name: "attended", permission: "default", wantBlock: false},
		{name: "yolo", permission: "bypassPermissions", wantBlock: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			featureIDCache = featureIDCacheEntry{}
			sessionID := "019ee378-abcd-7000-8000-" + uuid.New().String()[24:]
			projectDir := t.TempDir()
			wipnoteDir := filepath.Join(projectDir, ".wipnote")
			if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
				t.Fatalf("mkdir .wipnote: %v", err)
			}
			t.Setenv("CLAUDE_PROJECT_DIR", "")
			t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
			openCanonicalClaim(t, wipnoteDir, sessionID, sessionID, db.AgentRootSentinel, "feat-research-posture")

			// One local Read: satisfies read-before-write, not web research.
			database := makeSessionDB(t, sessionID, projectDir)
			now := time.Now().UTC()
			if err := db.InsertEvent(database, &models.AgentEvent{
				EventID: uuid.New().String(), AgentID: "claude-code", EventType: models.EventToolCall,
				Timestamp: now, ToolName: "Read", SessionID: sessionID, Status: "recorded",
				Source: "hook", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				t.Fatalf("InsertEvent: %v", err)
			}

			result, err := PreToolUse(&CloudEvent{
				SessionID:      sessionID,
				CWD:            projectDir,
				PermissionMode: tc.permission,
				ToolName:       "Edit",
				ToolInput: map[string]any{
					"file_path":  filepath.Join(projectDir, "go.mod"),
					"old_string": "go 1.22",
					"new_string": "go 1.23",
				},
			}, database)
			if err != nil {
				t.Fatalf("PreToolUse: %v", err)
			}
			const msg = "Research is required before changing dependencies"
			if tc.wantBlock {
				if result == nil || result.Decision != "block" || !strings.Contains(result.Reason, msg) {
					t.Fatalf("yolo must block the manifest edit, got %+v", result)
				}
				return
			}
			if result == nil || result.Decision == "block" {
				t.Fatalf("attended session must not block, got %+v", result)
			}
			if !strings.Contains(result.AdditionalContext, msg) {
				t.Fatalf("attended session must advise in additionalContext, got %q", result.AdditionalContext)
			}
		})
	}
}
