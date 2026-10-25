package hooks

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
	worktreepkg "github.com/shakestzd/wipnote/core/worktree"
)

// setupMissingEventsDB creates a temp project dir with .wipnote/ and an
// active session, returning the database and the session ID.
func setupMissingEventsDB(t *testing.T) (*testDB, string) {
	t.Helper()
	td := setupTestDB(t)

	// Link the test session to a project dir so ResolveProjectDir works.
	projectDir := t.TempDir()
	t.Setenv("WIPNOTE_SESSION_ID", "test-sess")
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	return td, "test-sess"
}

func setupGitRepoForWorktreeCreate(t *testing.T) string {
	t.Helper()
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, ".wipnote"), 0o755); err != nil {
		t.Fatalf("mkdir .wipnote: %v", err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repoRoot
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("test\n"), 0o644); err != nil {
		t.Fatalf("write README.md: %v", err)
	}
	run("add", "README.md")
	run("commit", "-m", "initial")

	prev := worktreepkg.SetReindexFnForTest(func(string, io.Writer) {})
	t.Cleanup(func() { worktreepkg.SetReindexFnForTest(prev) })

	return repoRoot
}

// --- PreCompact ---

// TestWorktreeRemove_NoAdditionalContext verifies that the cleanup in
// feat-cd937fc9 removed the dead additionalContext return (CC ignores it for
// this Observational event) while keeping the checkpoint recording.
func TestWorktreeRemove_NoAdditionalContext(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: "/repo/.claude/worktrees/feat-aabbccdd",
	}

	result, err := WorktreeRemove(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeRemove: %v", err)
	}
	if result == nil || !result.Continue {
		t.Fatal("expected Continue=true from WorktreeRemove")
	}
	if result.AdditionalContext != "" {
		t.Errorf("expected no additionalContext (CC ignores it for Observational WorktreeRemove), got %q", result.AdditionalContext)
	}

}

// --- PermissionRequest ---

// TestConfigChange_RecordsPermissionMode verifies that ConfigChange persists
// the permission_mode where the YOLO fallback reads it. It used to be an UPDATE
// of the sessions.metadata JSON column in the per-project read index; that index
// is gone, so the record is now a per-session file under .wipnote/
// (feat-fc3cc9e0). The round trip is asserted through the reader the guards
// actually use, not by inspecting the file directly.
func TestConfigChange_RecordsPermissionMode(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	clearNestedEnv(t)
	projectDir := t.TempDir()
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	event := &CloudEvent{
		SessionID:      sessionID,
		CWD:            projectDir,
		PermissionMode: "bypassPermissions",
	}

	result, err := ConfigChange(event, td.DB)
	if err != nil {
		t.Fatalf("ConfigChange: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from ConfigChange")
	}

	if got := recordedSessionPermissionMode(wipnoteDir, sessionID); got != "bypassPermissions" {
		t.Errorf("expected recorded permission_mode=bypassPermissions, got %q", got)
	}
	if !isYoloFromRecordedMode(wipnoteDir, sessionID) {
		t.Error("recorded bypassPermissions must resolve as YOLO posture")
	}
}

// TestConfigChange_EmptyPermissionMode_NoUpdate verifies that ConfigChange
// returns Continue without DB update when PermissionMode is empty.
func TestConfigChange_EmptyPermissionMode_NoUpdate(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID:      sessionID,
		CWD:            t.TempDir(),
		PermissionMode: "",
	}

	result, err := ConfigChange(event, td.DB)
	if err != nil {
		t.Fatalf("ConfigChange: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from ConfigChange")
	}

	// Metadata should remain NULL (not set).
	var metadata interface{}
	if err := td.DB.QueryRow(
		`SELECT metadata FROM sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&metadata); err != nil {
		t.Fatalf("query sessions metadata: %v", err)
	}
	if metadata != nil {
		t.Errorf("expected metadata to remain NULL, got %v", metadata)
	}
}

// --- WorktreeCreate ---

// TestWorktreeCreate_NoPath_RecordsGenericSummary verifies missing replacement
// hook fields abort cleanly and record no generic checkpoint.
func TestWorktreeCreate_NoPath_RecordsGenericSummary(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
	}

	if _, err := WorktreeCreate(event, td.DB); err == nil {
		t.Fatal("expected missing WorktreeCreate fields to error")
	}

	var count int
	if err := td.DB.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("query agent_events count: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no WorktreeCreate checkpoint on missing fields, got %d", count)
	}
}

// TestWorktreeCreate_NilDB_StillCreatesAndReturnsPath verifies the
// canonical-first fallback (feat-075c110d): when the derived-index handle is
// unavailable (writer_unavailable → nil *sql.DB), WorktreeCreate MUST still
// create the worktree and return its bare path (the #119 stdout contract),
// skipping only the best-effort checkpoint write. It must NOT panic on the
// nil handle and must NOT error.
func TestWorktreeCreate_NilDB_StillCreatesAndReturnsPath(t *testing.T) {
	repoRoot := setupGitRepoForWorktreeCreate(t)
	basePath := filepath.Join(repoRoot, ".claude", "worktrees")
	worktreeName := "feat-deadbeef"
	worktreePath := filepath.Join(basePath, worktreeName)

	event := &CloudEvent{
		SessionID:        "sess-nil-db",
		CWD:              repoRoot,
		WorktreeBasePath: basePath,
		WorktreeName:     worktreeName,
	}

	got, err := WorktreeCreate(event, nil)
	if err != nil {
		t.Fatalf("WorktreeCreate with nil DB: %v", err)
	}
	if got != worktreePath {
		t.Fatalf("WorktreeCreate path = %q, want %q", got, worktreePath)
	}
	if info, err := os.Stat(got); err != nil || !info.IsDir() {
		t.Fatalf("created path is not a directory: info=%v err=%v", info, err)
	}
}

// TestWorktreeCreate_MissingFields_NilDB_StillErrors verifies the contract
// boundary: even on the fallback (nil DB) path, a genuine failure (missing
// worktree_name / worktree_base_path) must STILL return an error so the
// command exits non-zero with no path on stdout.
func TestWorktreeCreate_MissingFields_NilDB_StillErrors(t *testing.T) {
	event := &CloudEvent{SessionID: "sess-x", CWD: t.TempDir()}
	if got, err := WorktreeCreate(event, nil); err == nil {
		t.Fatalf("expected error on missing fields with nil DB, got path=%q nil err", got)
	}
}

// --- TaskCreated ---

// stubTaskStepFns replaces addTaskStepFn/completeTaskStepFn with counters for
// the duration of the test, restoring the real functions on cleanup — avoids
// shelling out to a real wipnote binary while observing whether a
// step-mirroring call happened.
func stubTaskStepFns(t *testing.T) (addCalls, completeCalls *int) {
	t.Helper()
	addCalls, completeCalls = new(int), new(int)
	prevAdd, prevComplete := addTaskStepFn, completeTaskStepFn
	addTaskStepFn = func(_, _, _, _ string) { *addCalls++ }
	completeTaskStepFn = func(_, _ string) { *completeCalls++ }
	t.Cleanup(func() {
		addTaskStepFn = prevAdd
		completeTaskStepFn = prevComplete
	})
	return addCalls, completeCalls
}

// TestSessionResume_NoSessionID_ReturnsContinue verifies that SessionResume
// returns Continue without error when there is no session ID.
func TestSessionResume_NoSessionID_ReturnsContinue(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)
	t.Setenv("WIPNOTE_SESSION_ID", "")

	event := &CloudEvent{SessionID: "", CWD: projectDir}
	result, err := SessionResume(event, database, projectDir)
	if err != nil {
		t.Fatalf("SessionResume: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true when no session ID")
	}
}

// --- Stop ---

// TestStop_FinalizesSessionHTML verifies that Stop calls FinalizeSessionHTML
// so the event-count badge is correct for harnesses (e.g. Codex) that map
// their task-complete event to this handler instead of SessionEnd.
func TestStop_FinalizesSessionHTML(t *testing.T) {
	projectDir := t.TempDir()
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	database, err := db.Open(filepath.Join(t.TempDir(), "wipnote.db"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	sessionID := "stop-finalize-test-001"
	sess := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "codex",
		Status:        "active",
		CreatedAt:     time.Now().UTC(),
	}
	if err := db.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	// Create session HTML file so FinalizeSessionHTML has something to update.
	CreateSessionHTML(projectDir, sess)

	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_AGENT_TYPE", "codex")
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")

	event := &CloudEvent{SessionID: sessionID, CWD: projectDir}
	result, err := Stop(event, database)
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from Stop")
	}

	htmlPath := filepath.Join(sessionsDir, sessionID+".html")
	data, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("read session HTML: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `data-status="completed"`) {
		t.Error("Stop should have set data-status=completed in session HTML")
	}
	if strings.Contains(content, `data-status="active"`) {
		t.Error("data-status should NOT still be active after Stop")
	}
}

// --- StopFailure ---

// TestCloudEvent_NonStringErrorDoesNotBreakDecode verifies that an `error`
// key holding an object (as another harness might send) still decodes the
// rest of the event.
func TestCloudEvent_NonStringErrorDoesNotBreakDecode(t *testing.T) {
	payload := `{"session_id":"s1","tool_name":"Bash","error":{"code":7,"message":"boom"}}`
	var event CloudEvent
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if event.SessionID != "s1" || event.ToolName != "Bash" {
		t.Errorf("decoded session/tool = %q/%q", event.SessionID, event.ToolName)
	}
	if string(event.StopError) != `{"code":7,"message":"boom"}` {
		t.Errorf("StopError = %q", event.StopError)
	}
}

// TestStopFailure_PersistsToSessionHTMLWithoutDB verifies StopFailure writes
// the canonical session HTML entry before (and without) the derived SQLite
// insert, so a reindex can rebuild the event and a failed DB open loses nothing.
func TestStopFailure_PersistsToSessionHTMLWithoutDB(t *testing.T) {
	projectDir := t.TempDir()
	sessionsDir := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessionID := "stop-failure-canonical-001"
	CreateSessionHTML(projectDir, &models.Session{
		SessionID: sessionID, AgentAssigned: "claude", Status: "active", CreatedAt: time.Now().UTC(),
	})
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)

	event := &CloudEvent{SessionID: sessionID, CWD: projectDir, StopError: "overloaded"}
	result, err := StopFailure(event, nil)
	if err != nil {
		t.Fatalf("StopFailure: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from StopFailure")
	}

	data, err := os.ReadFile(filepath.Join(sessionsDir, sessionID+".html"))
	if err != nil {
		t.Fatalf("read session HTML: %v", err)
	}
	content := string(data)
	for _, want := range []string{`data-tool="StopFailure"`, `data-success="false"`, `data-status="failed"`, "Turn ended on API error: overloaded"} {
		if !strings.Contains(content, want) {
			t.Errorf("session HTML missing %q", want)
		}
	}
}
