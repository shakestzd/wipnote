package hooks

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// splitDBHarness runs the PreToolUse → PostToolUse pair the way production
// dispatches them: with no database handle (feat-174dc958). Everything a test
// checks is canonical: the tool log and the session HTML.
type splitDBHarness struct {
	projectDir string
	sessionID  string
}

func newSplitDBHarness(t *testing.T, sessionID string) *splitDBHarness {
	t.Helper()
	clearNestedEnv(t)
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote", "sessions"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_AGENT_ID", "claude-code")
	t.Setenv("WIPNOTE_AGENT_TYPE", "")
	t.Setenv("WIPNOTE_PARENT_EVENT", "")
	t.Setenv("WIPNOTE_PARENT_PROMPT_EVENT", "")
	t.Setenv("WIPNOTE_DB_PATH", filepath.Join(t.TempDir(), "never-opened.db"))
	CreateSessionHTML(projectDir, &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	})
	return &splitDBHarness{projectDir: projectDir, sessionID: sessionID}
}

// runPre dispatches PreToolUse with no handle, the way hookSubcmdReadOnly
// does in production.
func (h *splitDBHarness) runPre(t *testing.T, ev *CloudEvent) *HookResult {
	t.Helper()
	var ro *sql.DB // hot hooks dispatch with no handle (feat-af8d1689)
	res, err := PreToolUse(ev, ro)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if res != nil && res.Decision == "block" {
		t.Fatalf("PreToolUse blocked unexpectedly: %s", res.Reason)
	}
	return res
}

// runPost dispatches PostToolUse with no handle, the way hookSubcmdReadOnly
// dispatches it in production.
func (h *splitDBHarness) runPost(t *testing.T, ev *CloudEvent) *HookResult {
	t.Helper()
	var ro *sql.DB // hot hooks dispatch with no handle (feat-af8d1689)
	res, err := PostToolUse(ev, ro)
	if err != nil {
		t.Fatalf("PostToolUse: %v", err)
	}
	return res
}

func (h *splitDBHarness) sessionHTML(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(h.projectDir, ".wipnote", "sessions", h.sessionID+".html"))
	if err != nil {
		t.Fatalf("read session html: %v", err)
	}
	return string(b)
}

// writeOversizedFile writes a file long enough to trip CheckFileQuality's
// module-size warning.
func writeOversizedFile(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "big.go")
	var sb strings.Builder
	sb.WriteString("package big\n")
	for i := 0; i < 700; i++ {
		sb.WriteString("var _ = 1\n")
	}
	if err := os.WriteFile(p, []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write big file: %v", err)
	}
	return p
}

// TestPostToolUse_ReadOnlyHandle_SideEffectsRun: posttooluse with no handle
// returns the quality warning, appends the session HTML and writes the
// completion line for the call (bug-07fd4a51, feat-174dc958).
func TestPostToolUse_ReadOnlyHandle_SideEffectsRun(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-ro-001")
	bigFile := writeOversizedFile(t, t.TempDir())
	input := map[string]any{"file_path": bigFile, "content": "x"}
	ev := &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Write", ToolUseID: "toolu_ro_001", ToolInput: input}
	h.runPre(t, ev)

	post := *ev
	post.ToolResult = map[string]any{"output": "ok"}
	res := h.runPost(t, &post)
	if res == nil || !strings.Contains(res.AdditionalContext, "[Quality]") {
		t.Errorf("file-quality warning missing: %+v", res)
	}
	if !strings.Contains(h.sessionHTML(t), `data-event-id="`+toolCallEventID(h.sessionID, "toolu_ro_001")+`"`) {
		t.Errorf("session HTML entry missing or not keyed by the shared event id")
	}
	if got := toolLogDone(t, h, "toolu_ro_001"); got != "completed" {
		t.Errorf("tool-log completion = %q, want completed", got)
	}
}

// TestPostToolUseForHarness_ClaudeSplit: on Claude the sync `posttooluse`
// subcommand only responds (the async posttooluse-record hook records), while
// PostToolUseRecord records and never returns context. Codex runs both jobs.
func TestPostToolUseForHarness_ClaudeSplit(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-harness-001")
	bigFile := writeOversizedFile(t, t.TempDir())
	mk := func(id string, harness Harness) *CloudEvent {
		return &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Write", ToolUseID: id,
			ToolInput: map[string]any{"file_path": bigFile, "content": "x"}, ToolResult: map[string]any{"output": "ok"}, Harness: harness}
	}
	status := func(id string) string { return toolLogDone(t, h, id) }
	ro := func() *sql.DB { return nil } // hot hooks dispatch with no handle (feat-af8d1689)
	h.runPre(t, mk("toolu_claude", HarnessClaude))
	h.runPre(t, mk("toolu_codex", HarnessCodex))

	// Claude respond half: warning, but no completion and no HTML entry.
	res, _ := PostToolUseForHarness(mk("toolu_claude", HarnessClaude), ro())
	if !strings.Contains(res.AdditionalContext, "[Quality]") {
		t.Errorf("claude posttooluse lost the quality warning: %+v", res)
	}
	if status("toolu_claude") != "" || strings.Contains(h.sessionHTML(t), toolCallEventID(h.sessionID, "toolu_claude")) {
		t.Error("claude posttooluse must not record; posttooluse-record does")
	}

	// Claude record half: completion + HTML, no context.
	res, _ = PostToolUseRecord(mk("toolu_claude", HarnessClaude), ro())
	if res.AdditionalContext != "" || res.Decision != "" {
		t.Errorf("posttooluse-record must never return context or a decision: %+v", res)
	}
	if status("toolu_claude") != "completed" || !strings.Contains(h.sessionHTML(t), toolCallEventID(h.sessionID, "toolu_claude")) {
		t.Error("posttooluse-record did not record the call")
	}

	// Codex: one handler does both.
	res, _ = PostToolUseForHarness(mk("toolu_codex", HarnessCodex), ro())
	if !strings.Contains(res.AdditionalContext, "[Quality]") || status("toolu_codex") != "completed" {
		t.Errorf("codex posttooluse must both respond and record: res=%+v status=%q", res, status("toolu_codex"))
	}
}

// toolLogDone is the completion status the canonical tool log records for a
// call, or "" when it records none.
func toolLogDone(t *testing.T, h *splitDBHarness, toolUseID string) string {
	t.Helper()
	data, err := os.ReadFile(canonicalToolLogPath(h.projectDir, h.sessionID))
	if err != nil {
		return ""
	}
	id := toolCallEventID(h.sessionID, toolUseID)
	done := ""
	for _, line := range strings.Split(string(data), "\n") {
		var rec toolLogLine
		if json.Unmarshal([]byte(line), &rec) == nil && rec.ID == id && rec.Done != "" {
			done = rec.Done
		}
	}
	return done
}
