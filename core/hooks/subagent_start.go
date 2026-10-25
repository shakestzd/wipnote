package hooks

import (
	"database/sql"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/paths"
)

// SubagentStart handles the SubagentStart Claude Code hook event. It mints the
// delegation's event id and writes it, with the subagent's identity, where
// the subagent's own hooks read them: the traceparent file and the env vars.
// It writes no database rows (feat-174dc958). database is unused.
func SubagentStart(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	_ = database
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	agentType := event.AgentType
	if agentType == "" {
		agentType = "general-purpose"
	}
	eventID := uuid.New().String()

	// Write traceparent so the subagent's session-start can claim it.
	writeTraceparent(sessionID, eventID)
	// Write env vars so subagent hooks know their parent and identity.
	writeSubagentEnvVars(eventID, event.AgentID, agentType, projectDir, sessionID)
	return &HookResult{Continue: true}, nil
}

// SubagentStop handles the SubagentStop Claude Code hook event. It removes the
// per-subagent hint file SubagentStart wrote. It is observe-only: CC would
// read {decision:"block"} here, but there is no use-case to keep a subagent
// running. database is unused.
func SubagentStop(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	_ = database
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID != "" && event.AgentID != "" {
		paths.CleanupSubagentHint(sessionID, event.AgentID)
	}
	return &HookResult{Continue: true}, nil
}
