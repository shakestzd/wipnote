package hooks

import (
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/models"
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

// insertSessionWithID inserts a session with the given ID into a database.
func insertSessionWithID(t *testing.T, database interface {
	Exec(string, ...any) (interface{}, error)
}, sessionID, projectDir string) {
	t.Helper()
}

// --- PreCompact ---

// TestPreCompact_RecordsCheckpoint verifies that PreCompact records a
// checkpoint event with the expected tool_name.
func TestPreCompact_RecordsCheckpoint(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
	}

	result, err := PreCompact(event, td.DB)
	if err != nil {
		t.Fatalf("PreCompact: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from PreCompact")
	}

	var count int
	if err := td.DB.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'PreCompact'`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 PreCompact event, got %d", count)
	}
}

// TestPreCompact_NoSessionID_ReturnsContinue verifies that PreCompact returns
// Continue without error when there is no session ID.
func TestPreCompact_NoSessionID_ReturnsContinue(t *testing.T) {
	td := setupTestDB(t)
	t.Setenv("WIPNOTE_SESSION_ID", "")

	event := &CloudEvent{SessionID: "", CWD: t.TempDir()}
	result, err := PreCompact(event, td.DB)
	if err != nil {
		t.Fatalf("PreCompact: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true when no session ID")
	}
}

// --- InstructionsLoaded ---

// TestInstructionsLoaded_RecordsCheckpoint verifies that InstructionsLoaded
// records a checkpoint event with the expected tool_name.
func TestInstructionsLoaded_RecordsCheckpoint(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
	}

	result, err := InstructionsLoaded(event, td.DB)
	if err != nil {
		t.Fatalf("InstructionsLoaded: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from InstructionsLoaded")
	}

	var count int
	if err := td.DB.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND tool_name = 'InstructionsLoaded'`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 InstructionsLoaded event, got %d", count)
	}
}

// --- PermissionRequest ---

// TestPermissionRequest_RecordsCheckpoint verifies PermissionRequest records
// a checkpoint with the tool name in the summary.
func TestPermissionRequest_RecordsCheckpoint(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		ToolName:  "Bash",
	}

	result, err := PermissionRequest(event, td.DB)
	if err != nil {
		t.Fatalf("PermissionRequest: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from PermissionRequest")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'PermissionRequest'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Permission requested for tool: Bash" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// TestPermissionRequest_NoToolName_RecordsGenericSummary verifies that when
// no tool name is present, a generic summary is recorded.
func TestPermissionRequest_NoToolName_RecordsGenericSummary(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
	}

	result, err := PermissionRequest(event, td.DB)
	if err != nil {
		t.Fatalf("PermissionRequest: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from PermissionRequest")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'PermissionRequest'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Permission requested" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// --- ConfigChange ---

// TestConfigChange_UpdatesSessionMetadata verifies that ConfigChange upserts
// the permission_mode into the sessions.metadata JSON column.
func TestConfigChange_UpdatesSessionMetadata(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID:      sessionID,
		CWD:            t.TempDir(),
		PermissionMode: "bypassPermissions",
	}

	result, err := ConfigChange(event, td.DB)
	if err != nil {
		t.Fatalf("ConfigChange: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from ConfigChange")
	}

	var metadata string
	if err := td.DB.QueryRow(
		`SELECT COALESCE(metadata, '{}') FROM sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&metadata); err != nil {
		t.Fatalf("query sessions metadata: %v", err)
	}
	if metadata == "{}" {
		t.Error("expected metadata to be updated, still empty object")
	}

	// Verify the permission_mode was stored.
	var permMode string
	if err := td.DB.QueryRow(
		`SELECT json_extract(metadata, '$.permission_mode') FROM sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&permMode); err != nil {
		t.Fatalf("query permission_mode from metadata: %v", err)
	}
	if permMode != "bypassPermissions" {
		t.Errorf("expected permission_mode=bypassPermissions, got %q", permMode)
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

// TestWorktreeCreate_RecordsCheckpoint verifies that WorktreeCreate records
// a checkpoint event with the worktree path in the summary.
func TestWorktreeCreate_RecordsCheckpoint(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)
	worktreePath := "/repo/.claude/worktrees/feat-aabbccdd"

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		WorktreePath: worktreePath,
	}

	result, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from WorktreeCreate")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	expected := "Worktree created: " + worktreePath
	if inputSummary != expected {
		t.Errorf("unexpected input_summary: got %q, want %q", inputSummary, expected)
	}
}

// TestWorktreeCreate_NoPath_RecordsGenericSummary verifies a generic summary
// is recorded when no worktree path is provided.
func TestWorktreeCreate_NoPath_RecordsGenericSummary(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
	}

	result, err := WorktreeCreate(event, td.DB)
	if err != nil {
		t.Fatalf("WorktreeCreate: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from WorktreeCreate")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'WorktreeCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Worktree created" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// --- TaskCreated ---

// TestTaskCreated_RecordsEventWithSubject verifies that TaskCreated records a
// checkpoint event with the task subject in the summary.
func TestTaskCreated_RecordsEventWithSubject(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-001",
		TaskData:  map[string]any{"subject": "Run tests"},
	}

	result, err := TaskCreated(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCreated: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from TaskCreated")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TaskCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Task created: Run tests" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// TestTaskCreated_NoSubject_FallsBackToTaskID verifies that when no subject is
// provided, the task ID is used in the summary.
func TestTaskCreated_NoSubject_FallsBackToTaskID(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-xyz",
		TaskData:  map[string]any{},
	}

	result, err := TaskCreated(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCreated: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from TaskCreated")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TaskCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Task created: task_id=task-xyz" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// --- TaskCompleted ---

// TestTaskCompleted_RecordsEventWithSubject verifies that TaskCompleted records
// a task_completed event with the subject in the summary.
func TestTaskCompleted_RecordsEventWithSubject(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-001",
		TaskData:  map[string]any{"subject": "Run tests"},
	}

	result, err := TaskCompleted(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCompleted: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from TaskCompleted")
	}

	var inputSummary, status string
	if err := td.DB.QueryRow(
		`SELECT input_summary, status FROM agent_events WHERE session_id = ? AND tool_name = 'TaskComplete'`,
		sessionID,
	).Scan(&inputSummary, &status); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Task completed: Run tests" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
	if status != "completed" {
		t.Errorf("expected status=completed, got %q", status)
	}
}

// TestTaskCompleted_NoSubject_FallsBackToTaskID verifies that when no subject
// is provided, the task ID is used in the summary.
func TestTaskCompleted_NoSubject_FallsBackToTaskID(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-abc",
		TaskData:  map[string]any{},
	}

	result, err := TaskCompleted(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCompleted: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from TaskCompleted")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TaskComplete'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if inputSummary != "Task completed: task_id=task-abc" {
		t.Errorf("unexpected input_summary: %q", inputSummary)
	}
}

// --- TeammateIdle (Agent Teams) ---

// TestTeammateIdle_RecordsTeammateName verifies that when a teammate name and
// idle reason are present, the summary includes both.
func TestTeammateIdle_RecordsTeammateName(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID:    sessionID,
		CWD:          t.TempDir(),
		TeammateName: "implementer",
		IdleReason:   "waiting",
	}

	result, err := TeammateIdle(event, td.DB)
	if err != nil {
		t.Fatalf("TeammateIdle: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TeammateIdle'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query: %v", err)
	}
	expected := "Teammate implementer went idle (reason: waiting)"
	if inputSummary != expected {
		t.Errorf("input_summary = %q, want %q", inputSummary, expected)
	}
}

// TestTeammateIdle_NoTeammate_GenericSummary verifies legacy behavior when
// no teammate fields are present.
func TestTeammateIdle_NoTeammate_GenericSummary(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{SessionID: sessionID, CWD: t.TempDir()}

	result, err := TeammateIdle(event, td.DB)
	if err != nil {
		t.Fatalf("TeammateIdle: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TeammateIdle'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query: %v", err)
	}
	if inputSummary != "Teammate agent went idle" {
		t.Errorf("input_summary = %q, want %q", inputSummary, "Teammate agent went idle")
	}
}

// --- TaskCreated (Agent Teams typed fields) ---

// TestTaskCreated_PrefersTypedSubject verifies that TaskSubject takes priority
// over TaskData["subject"] when both are present.
func TestTaskCreated_PrefersTypedSubject(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID:   sessionID,
		CWD:         t.TempDir(),
		TaskID:      "task-typed",
		TaskSubject: "Build widget",
		TaskData:    map[string]any{"subject": "Old subject"},
	}

	result, err := TaskCreated(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCreated: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TaskCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query: %v", err)
	}
	if inputSummary != "Task created: Build widget" {
		t.Errorf("input_summary = %q, want %q", inputSummary, "Task created: Build widget")
	}
}

// TestTaskCreated_FallsBackToTaskData verifies that when TaskSubject is empty,
// the handler falls back to TaskData["subject"].
func TestTaskCreated_FallsBackToTaskData(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-fallback",
		TaskData:  map[string]any{"subject": "Fallback subject"},
	}

	result, err := TaskCreated(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCreated: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true")
	}

	var inputSummary string
	if err := td.DB.QueryRow(
		`SELECT input_summary FROM agent_events WHERE session_id = ? AND tool_name = 'TaskCreate'`,
		sessionID,
	).Scan(&inputSummary); err != nil {
		t.Fatalf("query: %v", err)
	}
	if inputSummary != "Task created: Fallback subject" {
		t.Errorf("input_summary = %q, want %q", inputSummary, "Task created: Fallback subject")
	}
}

// --- TaskCreated (EventTaskCreated constant) ---

// TestTaskCompleted_EmptyFeatureID_SkipsQualityGate verifies that when no
// feature is actively claimed (featureID == ""), the quality gate block is
// skipped entirely — no quality_gate event is recorded and no BlockExit2Error
// is returned.
func TestTaskCompleted_EmptyFeatureID_SkipsQualityGate(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)
	// No active_work_items row inserted → cachedGetActiveFeatureID returns "".

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-nofeature",
		TaskData:  map[string]any{"subject": "No feature task"},
	}

	result, err := TaskCompleted(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCompleted returned unexpected error: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true when featureID is empty")
	}

	// No quality_gate event should be recorded.
	var count int
	if err := td.DB.QueryRow(
		`SELECT COUNT(*) FROM agent_events WHERE session_id = ? AND event_type = 'quality_gate'`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("query agent_events: %v", err)
	}
	if count != 0 {
		t.Errorf("expected 0 quality_gate events when featureID is empty, got %d", count)
	}
}

// TestTaskCreated_UsesEventTaskCreated verifies that TaskCreated records with
// event_type='task_created' instead of 'check_point'.
func TestTaskCreated_UsesEventTaskCreated(t *testing.T) {
	td, sessionID := setupMissingEventsDB(t)

	event := &CloudEvent{
		SessionID: sessionID,
		CWD:       t.TempDir(),
		TaskID:    "task-type-check",
		TaskData:  map[string]any{"subject": "Type check"},
	}

	_, err := TaskCreated(event, td.DB)
	if err != nil {
		t.Fatalf("TaskCreated: %v", err)
	}

	var eventType string
	if err := td.DB.QueryRow(
		`SELECT event_type FROM agent_events WHERE session_id = ? AND tool_name = 'TaskCreate'`,
		sessionID,
	).Scan(&eventType); err != nil {
		t.Fatalf("query: %v", err)
	}
	if eventType != "task_created" {
		t.Errorf("event_type = %q, want %q", eventType, "task_created")
	}
}

// --- SessionResume ---

// TestSessionResume_ReactivatesCompletedSession verifies that SessionResume
// sets a completed session back to active.
func TestSessionResume_ReactivatesCompletedSession(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)

	sessionID := "resume-test-session-001"
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)

	// Insert a completed session.
	if err := db.InsertSession(database, &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		Status:        "completed",
		CreatedAt:     time.Now().UTC(),
		ProjectDir:    projectDir,
	}); err != nil {
		t.Fatalf("InsertSession: %v", err)
	}

	event := &CloudEvent{SessionID: sessionID, CWD: projectDir}
	result, err := SessionResume(event, database, projectDir)
	if err != nil {
		t.Fatalf("SessionResume: %v", err)
	}
	if result == nil || !result.Continue {
		t.Error("expected Continue=true from SessionResume")
	}
	if result.AdditionalContext == "" {
		t.Error("expected AdditionalContext to be set (resume confirmation message)")
	}

	// Verify session is now active.
	sess, err := db.GetSession(database, sessionID)
	if err != nil || sess == nil {
		t.Fatalf("GetSession: %v", err)
	}
	if sess.Status != "active" {
		t.Errorf("expected session status=active after resume, got %q", sess.Status)
	}
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
