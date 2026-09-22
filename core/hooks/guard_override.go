package hooks

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// guardOverrideEnvVar is the operator-only kill switch that disables every
// PreToolUse guard. It exists for break-glass recovery by a human and is
// deliberately never named in any agent-visible block message (GH-#164): an
// agent that reads "to proceed anyway, export X" treats it as an instruction.
const guardOverrideEnvVar = "WIPNOTE_GUARDS_OFF"

// guardOverrideStderr is where the override warning is written; tests swap it.
var guardOverrideStderr io.Writer = os.Stderr

// guardOverrideEnabled reports whether the kill switch is set.
func guardOverrideEnabled() bool {
	return os.Getenv(guardOverrideEnvVar) == "1"
}

// guardOverrideToolName is the tool_name under which an honoured override is
// recorded. The agent_events.event_type CHECK constraint enumerates its values,
// so the record uses the observational check_point type (like PermissionRequest
// and InstructionsLoaded) and is distinguished by this tool_name.
const guardOverrideToolName = "GuardOverride"

// recordGuardOverride makes an honoured kill switch loud: a stderr warning,
// a debug-log line, and a GuardOverride check_point agent_event attributed to
// the session and its active work item. Best-effort — never blocks the hook.
func recordGuardOverride(event *CloudEvent, database *sql.DB) {
	toolName := event.ToolName
	if toolName == "" {
		toolName = "(unknown tool)"
	}
	fmt.Fprintf(guardOverrideStderr,
		"wipnote: WARNING %s=1 is set — all PreToolUse guards are disabled for %s. "+
			"This override is recorded as a %s event.\n",
		guardOverrideEnvVar, toolName, guardOverrideToolName)

	sessionID := resolveSessionIDWithHarness(event)
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	debugLog(projectDir, "[guard_override] %s=1 honoured tool=%s session=%s",
		guardOverrideEnvVar, toolName, sessionID)
	if database == nil || sessionID == "" {
		return
	}

	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:       uuid.New().String(),
		AgentID:       resolveAgentID(event),
		EventType:     models.EventCheckPoint,
		Timestamp:     now,
		ToolName:      guardOverrideToolName,
		InputSummary:  fmt.Sprintf("Guard override honoured: %s=1 disabled all PreToolUse guards for %s", guardOverrideEnvVar, toolName),
		OutputSummary: SummariseInput(event.ToolName, event.ToolInput),
		SessionID:     sessionID,
		FeatureID:     cachedGetActiveFeatureID(database, sessionID),
		Status:        "recorded",
		Source:        "hook",
		StepID:        event.ToolUseID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.InsertEvent(database, ev); err != nil {
		debugLog(projectDir, "[error] handler=PreToolUse guard_override insert: %v", err)
	}
}
