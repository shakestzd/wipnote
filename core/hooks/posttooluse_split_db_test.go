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
