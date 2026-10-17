package hooks

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/daemon/apply"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// splitDBHarness reproduces the production handle topology for the
// PreToolUse → PostToolUse pair (bug-07fd4a51):
//
//   - daemonDB stands in for the writer daemon's in-memory projection
//     (serve_child opens it with OpenEphemeralProjection). Every
//     routeSQLAsync call is applied there through the genuine
//     Encode → applier → Exec transport, i.e. the daemon is reachable.
//   - pretooluse runs on OpenHookDBReadOnly's empty, query_only projection,
//     exactly as hookSubcmdReadOnly dispatches it.
//   - posttooluse runs on a SEPARATE on-disk handle opened with OpenHookDB, as
//     hookSubcmd dispatches it.
//
// The old tests handed one writable DB to both handlers, which made the
// started row pretooluse wrote visible to posttooluse's lookup and hid the split.
type splitDBHarness struct {
	projectDir string
	sessionID  string
	daemonDB   *sql.DB
	diskDB     *sql.DB
	diskPath   string
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
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")

	diskPath := filepath.Join(t.TempDir(), "hook-disk.db")
	t.Setenv("WIPNOTE_DB_PATH", diskPath)

	daemonDB, err := db.OpenEphemeralProjection()
	if err != nil {
		t.Fatalf("open daemon projection: %v", err)
	}
	t.Cleanup(func() { daemonDB.Close() })
	// session-start routes the session row to the daemon in production; seed it
	// so agent_events' session FK is satisfied in the daemon projection.
	if err := db.InsertSession(daemonDB, &models.Session{
		SessionID: sessionID, AgentAssigned: "claude-code", Status: "active",
		CreatedAt: time.Now().UTC(), ProjectDir: projectDir,
	}); err != nil {
		t.Fatalf("seed daemon session: %v", err)
	}

	applier := apply.NewApplier(daemonDB)
	prev := routeSQLAsync
	t.Cleanup(func() { routeSQLAsync = prev })
	routeSQLAsync = func(_ string, sqlStmt string, args ...any) bool {
		payload, encErr := apply.Encode(apply.DerivedOp{Type: apply.OpTypeSQL, SQL: sqlStmt, Args: args})
		if encErr != nil {
			t.Fatalf("daemon encode: %v", encErr)
		}
		op, opErr := applier(daemon.Envelope{OpType: apply.OpTypeSQL, Payload: payload})
		if opErr != nil {
			t.Fatalf("daemon applier: %v", opErr)
		}
		// Best-effort like the real daemon: an apply error is not the hook's
		// problem, and some claim statements legitimately touch no rows.
		if err := op(context.Background()); err != nil {
			t.Logf("daemon apply: %v", err)
		}
		return true
	}

	diskDB, reason := OpenHookDB("posttooluse", sessionID, diskPath)
	if diskDB == nil {
		t.Fatalf("OpenHookDB: nil (reason=%s)", reason)
	}
	t.Cleanup(func() { diskDB.Close() })

	featureIDCache = featureIDCacheEntry{}

	CreateSessionHTML(projectDir, &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	})

	return &splitDBHarness{
		projectDir: projectDir,
		sessionID:  sessionID,
		daemonDB:   daemonDB,
		diskDB:     diskDB,
		diskPath:   diskPath,
	}
}

// runPre dispatches PreToolUse on a fresh read-only projection handle, the
// way hookSubcmdReadOnly does in production.
func (h *splitDBHarness) runPre(t *testing.T, ev *CloudEvent) *HookResult {
	t.Helper()
	ro, reason := OpenHookDBReadOnly("pretooluse", h.sessionID, h.diskPath)
	if ro == nil {
		t.Fatalf("OpenHookDBReadOnly: nil (reason=%s)", reason)
	}
	defer ro.Close()
	res, err := PreToolUse(ev, ro)
	if err != nil {
		t.Fatalf("PreToolUse: %v", err)
	}
	if res != nil && res.Decision == "block" {
		t.Fatalf("PreToolUse blocked unexpectedly: %s", res.Reason)
	}
	return res
}

// runPost dispatches PostToolUse on a fresh read-only projection handle, the
// way hookSubcmdReadOnly dispatches it in production since bug-07fd4a51 step 5.
func (h *splitDBHarness) runPost(t *testing.T, ev *CloudEvent) *HookResult {
	t.Helper()
	ro, reason := OpenHookDBReadOnly("posttooluse", h.sessionID, h.diskPath)
	if ro == nil {
		t.Fatalf("OpenHookDBReadOnly: nil (reason=%s)", reason)
	}
	defer ro.Close()
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

// TestPostToolUse_SplitDB_SideEffectsRun is the bug-07fd4a51 regression test:
// with pretooluse's started row in the daemon projection and posttooluse
// reading a separate disk handle, the started-row lookup misses. PostToolUse
// must still append to the session HTML and return the file-quality warning.
func TestPostToolUse_SplitDB_SideEffectsRun(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-sess-001")
	// Target lives outside the project root so the work-item write guard (which
	// only constrains project code) does not block the PreToolUse half.
	bigFile := writeOversizedFile(t, t.TempDir())

	input := map[string]any{"file_path": bigFile, "content": "x"}
	h.runPre(t, &CloudEvent{
		SessionID: h.sessionID, CWD: h.projectDir,
		ToolName: "Write", ToolUseID: "toolu_split_001", ToolInput: input,
	})

	// Precondition proving the split: the started row is in the daemon
	// projection and NOT on the disk handle posttooluse reads.
	var daemonRows, diskRows int
	_ = h.daemonDB.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'Write'`, h.sessionID).Scan(&daemonRows)
	_ = h.diskDB.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'Write'`, h.sessionID).Scan(&diskRows)
	if daemonRows != 1 || diskRows != 0 {
		t.Fatalf("precondition: want started row only in daemon projection, got daemon=%d disk=%d", daemonRows, diskRows)
	}

	res, err := PostToolUse(&CloudEvent{
		SessionID: h.sessionID, CWD: h.projectDir,
		ToolName: "Write", ToolUseID: "toolu_split_001", ToolInput: input,
		ToolResult: map[string]any{"output": "ok"},
	}, h.diskDB)
	if err != nil {
		t.Fatalf("PostToolUse: %v", err)
	}
	if res == nil || !strings.Contains(res.AdditionalContext, "[Quality]") {
		t.Errorf("file-quality warning missing from PostToolUse result: %+v", res)
	}
	if html := h.sessionHTML(t); !strings.Contains(html, `data-tool="Write"`) {
		t.Errorf("session HTML was not appended for the Write call")
	}
}

// TestPostToolUse_ReadOnlyHandle_SideEffectsRun is the step-5 counterpart of
// the regression test above: posttooluse on the read-only projection (its
// production handle now) still returns the quality warning, appends the
// session HTML and completes the daemon row.
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
	var status string
	_ = h.daemonDB.QueryRow(`SELECT status FROM agent_events WHERE event_id = ?`, toolCallEventID(h.sessionID, "toolu_ro_001")).Scan(&status)
	if status != "completed" {
		t.Errorf("daemon row status = %q, want completed", status)
	}
}

// TestPostToolUse_ReadOnlyHandle_DaemonMissFallsBackToDisk: with the daemon
// unreachable for both halves, pretooluse and posttooluse each fall back to
// their own bounded writable handle on the disk DB, and the completion
// upserts the started row there instead of failing on the read-only handle.
func TestPostToolUse_ReadOnlyHandle_DaemonMissFallsBackToDisk(t *testing.T) {
	h := newSplitDBHarness(t, "split-db-miss-001")
	stubRouteSQLAsync(t, false)
	if err := db.InsertSession(h.diskDB, &models.Session{
		SessionID: h.sessionID, AgentAssigned: "claude-code", Status: "active",
		CreatedAt: time.Now().UTC(), ProjectDir: h.projectDir,
	}); err != nil {
		t.Fatalf("seed disk session: %v", err)
	}
	in := map[string]any{"file_path": "/tmp/miss.go"}
	h.runPre(t, &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Read", ToolUseID: "toolu_miss", ToolInput: in})
	h.runPost(t, &CloudEvent{SessionID: h.sessionID, CWD: h.projectDir, ToolName: "Read", ToolUseID: "toolu_miss", ToolInput: in,
		ToolResult: map[string]any{"output": "x"}})

	r, ok := readEventRow(t, h.diskDB, toolCallEventID(h.sessionID, "toolu_miss"))
	if !ok || r.status != "completed" {
		t.Fatalf("disk row = %+v (found=%v), want completed", r, ok)
	}
	var n int
	_ = h.diskDB.QueryRow(`SELECT COUNT(*) FROM agent_events WHERE session_id = ?`, h.sessionID).Scan(&n)
	if n != 1 {
		t.Errorf("disk rows = %d, want 1 (upsert in place)", n)
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
	status := func(id string) string {
		var s string
		_ = h.daemonDB.QueryRow(`SELECT status FROM agent_events WHERE event_id = ?`, toolCallEventID(h.sessionID, id)).Scan(&s)
		return s
	}
	ro := func() *sql.DB {
		d, _ := OpenHookDBReadOnly("posttooluse", h.sessionID, h.diskPath)
		t.Cleanup(func() { d.Close() })
		return d
	}

	// Claude respond half: warning, but no row and no HTML entry.
	res, _ := PostToolUseForHarness(mk("toolu_claude", HarnessClaude), ro())
	if !strings.Contains(res.AdditionalContext, "[Quality]") {
		t.Errorf("claude posttooluse lost the quality warning: %+v", res)
	}
	if status("toolu_claude") != "" || strings.Contains(h.sessionHTML(t), toolCallEventID(h.sessionID, "toolu_claude")) {
		t.Error("claude posttooluse must not record; posttooluse-record does")
	}

	// Claude record half: row + HTML, no context.
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
