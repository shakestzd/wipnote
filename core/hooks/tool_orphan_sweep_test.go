package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
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
// and a timestamp; PostToolUse and PostToolUseFailure record a completion line
// under the same id. The sweep records "aborted" only for a start with no
// completion whose session has ended since it started, or that is over five
// minutes old in an open session whose owner process (.session-pid) is gone.
// A completed or failed call, a recent call, and an old call in a session
// whose owner is alive or unknown are left alone. A second pass records
// nothing new, and completion lines do not count as tool calls.
func TestOrphanToolSweep_PairsStartsWithCompletions(t *testing.T) {
	h := newSplitDBHarness(t, "orphan-sweep-dead-1")
	now := time.Now().UTC()
	old, recent := now.Add(-10*time.Minute), now.Add(-1*time.Minute)
	const closed, alive, unknown = "orphan-sweep-closed-1", "orphan-sweep-alive-1", "orphan-sweep-nopid-1"
	for _, sid := range []string{closed, alive, unknown} {
		CreateSessionHTML(h.projectDir, &models.Session{SessionID: sid, AgentAssigned: "claude-code",
			Status: "active", CreatedAt: now, ProjectDir: h.projectDir})
	}
	sessDir := func(sid string) string { return filepath.Join(h.projectDir, ".wipnote", "sessions", sid) }

	startToolAt(t, h, h.sessionID, "tu_paired", old)
	finishTool(t, h, h.sessionID, "tu_paired")
	startToolAt(t, h, h.sessionID, "tu_failed", old)
	failTool(t, h, h.sessionID, "tu_failed")
	startToolAt(t, h, h.sessionID, "tu_old", old)
	startToolAt(t, h, h.sessionID, "tu_new", recent)
	writeSessionPIDRaw(t, sessDir(h.sessionID), "2147483647\n1")
	startToolAt(t, h, closed, "tu_closed", recent)
	writeEndNote(t, h.projectDir, closed, now)
	startToolAt(t, h, alive, "tu_alive", old)
	writeLivePID(t, sessDir(alive))
	startToolAt(t, h, unknown, "tu_nopid", old)

	if got := SweepOrphanedToolCalls(h.projectDir, "orphan-sweep-new-1", now, SessionStartSweepCap); got != 2 {
		t.Errorf("first sweep recorded %d aborted calls, want 2", got)
	}
	for _, c := range []struct {
		session, toolUse string
		want             bool
	}{
		{h.sessionID, "tu_old", true}, {closed, "tu_closed", true},
		{h.sessionID, "tu_paired", false}, {h.sessionID, "tu_failed", false}, {h.sessionID, "tu_new", false},
		{alive, "tu_alive", false}, {unknown, "tu_nopid", false},
	} {
		if got := abortedInHTML(t, h.projectDir, c.session, c.toolUse); got != c.want {
			t.Errorf("%s/%s aborted=%v, want %v", c.session, c.toolUse, got, c.want)
		}
	}
	if again := SweepOrphanedToolCalls(h.projectDir, "orphan-sweep-new-1", now, SessionStartSweepCap); again != 0 {
		t.Errorf("second sweep recorded %d aborted calls, want 0", again)
	}
	if n := canonicalToolEventCount(h.projectDir, h.sessionID); n != 4 {
		t.Errorf("tool call count = %d, want 4 (completion lines are not tool calls)", n)
	}
}

// failTool runs the real PostToolUseFailure for one call.
func failTool(t *testing.T, h *splitDBHarness, sessionID, toolUseID string) {
	t.Helper()
	ev := &CloudEvent{SessionID: sessionID, CWD: h.projectDir, ToolName: "Read", ToolUseID: toolUseID,
		ToolResult: map[string]any{"error": "boom"}}
	if _, err := PostToolUseFailure(ev); err != nil {
		t.Fatalf("PostToolUseFailure: %v", err)
	}
}

// writeLivePID anchors sessDir to this test process, which is alive.
func writeLivePID(t *testing.T, sessDir string) {
	t.Helper()
	content := strconv.Itoa(os.Getpid())
	if st, ok := readProcStartTime(os.Getpid()); ok {
		content += "\n" + strconv.FormatUint(st, 10)
	}
	writeSessionPIDRaw(t, sessDir, content)
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

// TestOrphanToolSweep_OneSweeperPerLog pins that two session starts sweeping
// at once record an orphan once: a sweep skips a tool log another sweep is
// holding, and records it once the log is free.
func TestOrphanToolSweep_OneSweeperPerLog(t *testing.T) {
	h := newSplitDBHarness(t, "orphan-sweep-lock-1")
	now := time.Now().UTC()
	startToolAt(t, h, h.sessionID, "tu_locked", now.Add(-10*time.Minute))
	writeSessionPIDRaw(t, filepath.Join(h.projectDir, ".wipnote", "sessions", h.sessionID), "2147483647\n1")

	f, err := os.Open(canonicalToolLogPath(h.projectDir, h.sessionID))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatal(err)
	}
	if got := SweepOrphanedToolCalls(h.projectDir, "", now, 0); got != 0 {
		t.Errorf("sweep recorded %d while another sweep held the log, want 0", got)
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	if got := SweepOrphanedToolCalls(h.projectDir, "", now, 0); got != 1 {
		t.Errorf("sweep recorded %d once the log was free, want 1", got)
	}
}
