package hooks

import (
	"os"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/paths"
)

// SubagentStart handles the SubagentStart Claude Code hook event. It mints the
// delegation's event id and writes it, with the subagent's identity, where
// the subagent's own hooks read them: the traceparent file and the env vars.
// It writes no database rows (feat-174dc958).
func SubagentStart(event *CloudEvent) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	recordDelegateHook(event, projectDir, sessionID, "subagent_start")
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

// SubagentStop handles the SubagentStop Claude Code hook event. It records
// calls denied inside the subagent as failed, from the subagent's own
// transcript (feat-ddeaa0d1), and removes the per-subagent hint file
// SubagentStart wrote. It is observe-only: CC would read {decision:"block"}
// here, but there is no use-case to keep a subagent running.
func SubagentStop(event *CloudEvent) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID != "" && event.AgentID != "" && os.Getenv(DelegateRunEnv) != "" {
		recordDelegateHook(event, ResolveProjectDir(event.CWD, event.SessionID), sessionID, "subagent_stop")
	}
	if sessionID != "" && event.AgentTranscriptPath != "" && event.Harness == HarnessClaude {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		recordClaudeTranscriptFailures(event, projectDir, sessionID, event.AgentTranscriptPath)
	}
	if sessionID != "" && event.AgentID != "" {
		paths.CleanupSubagentHint(sessionID, event.AgentID)
	}
	return &HookResult{Continue: true}, nil
}
