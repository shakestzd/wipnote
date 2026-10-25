package hooks

import (
	"os"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/models"
)

// TestSubagentStart_Idempotent asserts re-delivery of the same start event
// is safe — the INSERT OR IGNORE path on sessions plus a duplicate-PK warn
// on agent_lineage_trace must not fail the hook.
func TestSubagentStart_Idempotent(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)
	parentSessionID := "parent-idempotent"
	subagentID := "subagent-idempotent"

	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_SESSION_ID", parentSessionID)

	if err := dbtest.UpsertSession(database, &models.Session{
		SessionID: parentSessionID, AgentAssigned: "claude-code", Status: "active",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	event := &CloudEvent{
		SessionID: parentSessionID,
		CWD:       projectDir,
		AgentID:   subagentID,
		AgentType: "general-purpose",
	}
	if _, err := SubagentStart(event); err != nil {
		t.Fatalf("SubagentStart first: %v", err)
	}
	if _, err := SubagentStart(event); err != nil {
		t.Fatalf("SubagentStart re-delivery: %v", err)
	}
}

// TestSubagentStart_WritesOTELResourceAttributes asserts that writeSubagentEnvVars
// appends OTEL_RESOURCE_ATTRIBUTES=wipnote.agent_id=<id> to CLAUDE_ENV_FILE,
// merging with any existing value so pre-existing attributes are preserved.
func TestSubagentStart_WritesOTELResourceAttributes(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)
	parentSessionID := "parent-otel-ra"
	subagentID := "subagent-otel-ra-456"

	// Create a temp env file that CLAUDE_ENV_FILE points to.
	// writeSubagentEnvVars uses O_APPEND|O_WRONLY (no O_CREATE), so the file
	// must already exist — it's normally pre-created by writeEnvVars in SessionStart.
	envFile := t.TempDir() + "/claude.env"
	if err := os.WriteFile(envFile, nil, 0o644); err != nil {
		t.Fatalf("create CLAUDE_ENV_FILE: %v", err)
	}
	t.Setenv("CLAUDE_ENV_FILE", envFile)
	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_SESSION_ID", parentSessionID)
	t.Setenv("WIPNOTE_AGENT_ID", "claude-code")
	t.Setenv("WIPNOTE_PARENT_EVENT", "")
	// Pre-set an existing OTEL_RESOURCE_ATTRIBUTES value to verify merge.
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=test,service.version=1.0")

	if err := dbtest.UpsertSession(database, &models.Session{
		SessionID:     parentSessionID,
		AgentAssigned: "claude-code",
		Status:        "active",
	}); err != nil {
		t.Fatalf("UpsertSession parent: %v", err)
	}

	event := &CloudEvent{
		SessionID: parentSessionID,
		CWD:       projectDir,
		AgentID:   subagentID,
		AgentType: "general-purpose",
	}
	if _, err := SubagentStart(event); err != nil {
		t.Fatalf("SubagentStart: %v", err)
	}

	// Read env file contents and look for OTEL_RESOURCE_ATTRIBUTES.
	content, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read CLAUDE_ENV_FILE: %v", err)
	}
	fileStr := string(content)

	expectedSubstr := "wipnote.agent_id=" + subagentID
	if !strings.Contains(fileStr, expectedSubstr) {
		t.Errorf("CLAUDE_ENV_FILE does not contain %q\n\nfile contents:\n%s", expectedSubstr, fileStr)
	}
	// The existing attrs should be preserved (merged, not clobbered).
	if !strings.Contains(fileStr, "service.name=test") {
		t.Errorf("CLAUDE_ENV_FILE lost pre-existing OTEL_RESOURCE_ATTRIBUTES; file:\n%s", fileStr)
	}
}

// TestSubagentStop_MissingTraceIsNonFatal asserts a stop event with no
// matching start row does not return an error (log-and-continue semantics).
func TestSubagentStop_MissingTraceIsNonFatal(t *testing.T) {
	database, projectDir := setupLifecycleDB(t)
	parentSessionID := "parent-no-trace"

	t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
	t.Setenv("WIPNOTE_SESSION_ID", parentSessionID)

	if err := dbtest.UpsertSession(database, &models.Session{
		SessionID: parentSessionID, AgentAssigned: "claude-code", Status: "active",
	}); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	stopEvent := &CloudEvent{
		SessionID: parentSessionID,
		CWD:       projectDir,
		AgentID:   "subagent-never-started",
	}
	if _, err := SubagentStop(stopEvent); err != nil {
		t.Fatalf("SubagentStop (missing trace) should not error: %v", err)
	}
}
