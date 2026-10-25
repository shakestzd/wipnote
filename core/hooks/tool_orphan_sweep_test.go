package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// startToolAt runs the real PreToolUse for one call with the tool-log clock
// set to at, so the start line carries that timestamp.
func startToolAt(t *testing.T, h *splitDBHarness, sessionID, toolUseID string, at time.Time) {
	t.Helper()
	prev := toolLogNow
	toolLogNow = func() time.Time { return at }
	defer func() { toolLogNow = prev }()
	h.runPre(t, &CloudEvent{SessionID: sessionID, CWD: h.projectDir, ToolName: "Read",
		ToolUseID: toolUseID, ToolInput: map[string]any{"file_path": "a.go"}})
}

func finishTool(t *testing.T, h *splitDBHarness, sessionID, toolUseID string) {
	t.Helper()
	h.runPost(t, &CloudEvent{SessionID: sessionID, CWD: h.projectDir, ToolName: "Read",
		ToolUseID: toolUseID, ToolInput: map[string]any{"file_path": "a.go"},
		ToolResult: map[string]any{"output": "ok"}})
}

func abortedInHTML(t *testing.T, projectDir, sessionID, toolUseID string) bool {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatalf("read session html: %v", err)
	}
	id := regexp.QuoteMeta(toolCallEventID(sessionID, toolUseID))
	return regexp.MustCompile(`data-event-id="` + id + `"[^>]*data-status="aborted"`).Match(b)
}

// TestOrphanToolSweep_PairsStartsWithCompletions pins the session-start orphan
// sweep over the canonical tool log (feat-174dc958). PreToolUse records an id
// and a timestamp, PostToolUse a completion line under the same id. The sweep
// records "aborted" only for a start with no completion that is over five
// minutes old or whose session has ended; a completed call or a recent call in
// a live session is left alone. A second pass records nothing new, and the
// completion lines do not count as tool calls.
func TestOrphanToolSweep_PairsStartsWithCompletions(t *testing.T) {
	h := newSplitDBHarness(t, "orphan-sweep-open-1")
	now := time.Now().UTC()
	const closed = "orphan-sweep-closed-1"
	CreateSessionHTML(h.projectDir, &models.Session{SessionID: closed, AgentAssigned: "claude-code",
		Status: "active", CreatedAt: now, ProjectDir: h.projectDir})

	startToolAt(t, h, h.sessionID, "tu_paired", now.Add(-10*time.Minute))
	finishTool(t, h, h.sessionID, "tu_paired")
	startToolAt(t, h, h.sessionID, "tu_old", now.Add(-10*time.Minute))
	startToolAt(t, h, h.sessionID, "tu_new", now.Add(-1*time.Minute))
	startToolAt(t, h, closed, "tu_closed", now.Add(-1*time.Minute))
	writeEndNote(t, h.projectDir, closed, now)

	if got := SweepOrphanedToolCalls(h.projectDir, "orphan-sweep-new-1", now, SessionStartSweepCap); got != 2 {
		t.Errorf("first sweep recorded %d aborted calls, want 2", got)
	}
	for _, c := range []struct {
		session, toolUse string
		want             bool
	}{
		{h.sessionID, "tu_old", true}, {closed, "tu_closed", true},
		{h.sessionID, "tu_paired", false}, {h.sessionID, "tu_new", false},
	} {
		if got := abortedInHTML(t, h.projectDir, c.session, c.toolUse); got != c.want {
			t.Errorf("%s/%s aborted=%v, want %v", c.session, c.toolUse, got, c.want)
		}
	}
	if again := SweepOrphanedToolCalls(h.projectDir, "orphan-sweep-new-1", now, SessionStartSweepCap); again != 0 {
		t.Errorf("second sweep recorded %d aborted calls, want 0", again)
	}
	if n := canonicalToolEventCount(h.projectDir, h.sessionID); n != 3 {
		t.Errorf("tool call count = %d, want 3 (completion lines are not tool calls)", n)
	}
}

func writeEndNote(t *testing.T, projectDir, sessionID string, endedAt time.Time) {
	t.Helper()
	dir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	line := `{"session_id":"` + sessionID + `","ended_at":"` + endedAt.Format(time.RFC3339Nano) + `","outcome":"abandoned"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, SessionEndNoteFile), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}
