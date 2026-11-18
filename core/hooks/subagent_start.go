package hooks

import (
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/paths"
)

// WorkerRules is the worker rules block. The main package sets it from the
// embedded prompts/worker-rules.md, because core cannot import cmd. A wipnote
// subagent gets no CLAUDE.md, so SubagentStart hands it the block as context
// in place of the two skills the agent files used to preload (feat-0b0112a4).
var WorkerRules string

// workerRulesFor returns the block for a wipnote agent type, and "" for any
// other plugin's agent, which has not asked for wipnote's rules.
func workerRulesFor(agentType string) string {
	if !strings.HasPrefix(agentType, "wipnote:") {
		return ""
	}
	return strings.TrimSpace(WorkerRules)
}

// SubagentStart handles the SubagentStart Claude Code hook event. It mints the
// delegation's event id and writes it, with the subagent's identity, where
// the subagent's own hooks read them: the traceparent file and the env vars.
// It writes no database rows (feat-174dc958).
func SubagentStart(event *CloudEvent) (*HookResult, error) {
	result := &HookResult{Continue: true, AdditionalContext: workerRulesFor(event.AgentType)}
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		return result, nil
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
	return result, nil
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
