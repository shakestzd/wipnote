package hooks

import (
	"database/sql"
	"strings"

	"github.com/shakestzd/wipnote/core/db"
)

func checkOrchestratorResearchDelegationAdvisory(event *CloudEvent, ctx *toolUseContext, database *sql.DB) string {
	if event == nil || ctx == nil || ctx.IsSubagent || ctx.SessionID == "" {
		return ""
	}
	if !isOrchestratorResearchTool(event.ToolName) {
		return ""
	}
	// A nil database is the hot-hook dispatch: the evidence comes from the
	// session's canonical tool log (feat-af8d1689).
	counts, sidecar := canonicalOrchestrationEvidence(ctx.ProjectDir, ctx.SessionID)
	if database != nil {
		var err error
		if counts, err = db.CountEventsByTool(database, ctx.SessionID); err != nil {
			return ""
		}
		sidecar = hasPriorSidecarCommand(database, ctx.SessionID)
	}
	if hasPriorOrchestratorResearch(counts) || hasPriorDelegationEvidence(counts) || sidecar {
		return ""
	}
	return "wipnote orchestration advisory: root-session web/docs research is starting before any sidecar dispatch was recorded. In orchestrator mode, delegate web/docs research to a researcher/codebase sidecar when available. If this is a one-shot verification or no sidecar exists in this harness, continue and note that exception."
}

func isOrchestratorResearchTool(toolName string) bool {
	switch strings.TrimSpace(toolName) {
	case "WebSearch", "WebFetch", "web_search", "web_fetch", "google_web_search",
		"web.search_query", "web.open", "web.find", "web.click":
		return true
	default:
		return false
	}
}

func hasPriorOrchestratorResearch(counts map[string]int) bool {
	for toolName, count := range counts {
		if count > 0 && isOrchestratorResearchTool(toolName) {
			return true
		}
	}
	return false
}

func hasPriorDelegationEvidence(counts map[string]int) bool {
	// TaskStarted is intentionally excluded: in the Codex harness it is a generic
	// progress checkpoint (roborev finding 266, bug-60107613), not a sidecar spawn.
	// Counting it as delegation evidence caused the research advisory to be wrongly
	// suppressed before the first root web/docs tool call in Codex sessions.
	// Only genuine spawn events are accepted as delegation evidence.
	for _, toolName := range []string{
		"Task", "Agent", "SubagentStart", "multi_agent.spawn",
		"multi_agent_v1.spawn_agent", "multi_agent_v1.spawn_agents",
	} {
		if counts[toolName] > 0 {
			return true
		}
	}
	return false
}

func hasPriorSidecarCommand(database *sql.DB, sessionID string) bool {
	if database == nil {
		return false
	}
	var count int
	err := database.QueryRow(`
		SELECT COUNT(*) FROM agent_events
		WHERE session_id = ?
		  AND tool_name = 'Bash'
		  AND (
			input_summary LIKE 'gemini %'
			OR input_summary LIKE 'codex exec %'
			OR input_summary LIKE 'agy %'
			OR input_summary LIKE 'copilot %'
		  )
		LIMIT 1`, sessionID).Scan(&count)
	return err == nil && count > 0
}
