package hooks

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// collectRelatedSessionIDs builds a deduplicated slice of session IDs that
// are related to sessionID via two mechanisms:
//
//  1. Transitive parent walk: follows sessions.parent_session_id upward until
//     NULL or a cycle (capped at maxLineageHops hops).
//  2. Lineage trace fallback: for any session ID in the walking set, if an
//     agent_lineage_trace row exists with session_id = that ID, its
//     root_session_id is added too. This catches cases where
//     sessions.parent_session_id is NULL but SubagentStart wrote a trace row.
const maxLineageHops = 8

func collectRelatedSessionIDs(database *sql.DB, sessionID string) []string {
	if database == nil || sessionID == "" {
		return []string{sessionID}
	}
	seen := map[string]struct{}{}
	result := []string{}

	add := func(id string) bool {
		if id == "" {
			return false
		}
		if _, ok := seen[id]; ok {
			return false
		}
		seen[id] = struct{}{}
		result = append(result, id)
		return true
	}

	// Transitive parent walk.
	current := sessionID
	for hop := 0; hop <= maxLineageHops; hop++ {
		if !add(current) {
			break // cycle detected or already visited
		}
		var parentID string
		database.QueryRow(
			`SELECT COALESCE(parent_session_id, '') FROM sessions WHERE session_id = ?`,
			current,
		).Scan(&parentID)
		if parentID == "" {
			break
		}
		current = parentID
	}

	// Lineage trace fallback: for each ID we already have, check if it has a
	// trace row pointing to a root we haven't seen yet.
	snapshot := make([]string, len(result))
	copy(snapshot, result)
	for _, sid := range snapshot {
		add(lineageTraceRoot(database, sid)) // no-op if empty or already present
	}

	return result
}

// lineageTraceRoot returns the root_session_id of sid's agent_lineage_trace
// row, or "" when there is none.
func lineageTraceRoot(database *sql.DB, sid string) string {
	var rootID string
	database.QueryRow(
		`SELECT COALESCE(root_session_id, '') FROM agent_lineage_trace WHERE session_id = ? LIMIT 1`,
		sid,
	).Scan(&rootID)
	return rootID
}

// hasRecentResearch checks if Read/Grep/Glob (or equivalent research tools)
// were used in this session, any ancestor session, or by the same agent across
// sessions. It also handles two failure modes that previously caused misfires:
//
//  1. Agent-ID mismatch: sub-agent Reads are stored under the orchestrator's
//     session_id with the sub-agent's agent_id. The agentID parameter is used
//     as an additional match key so those events are found even when the
//     session walk misses them.
//
//  2. Orphaned sessions: when sessions.parent_session_id is NULL but a lineage
//     trace exists, collectRelatedSessionIDs follows the trace to the root.
//
// When the event recording pipeline is broken (zero tool_call events across all
// related IDs), the function fails open and emits a debug-log warning rather
// than silently blocking valid work.
func hasRecentResearch(database *sql.DB, sessionID, agentID, projectDir string) bool {
	if sessionID == "" {
		return true // fail-open: can't verify, don't block
	}
	// A nil database is the hot-hook dispatch (feat-af8d1689): it answers as the
	// empty projection it replaced, which recorded no tool calls.
	if database != nil {
		if researched, recorded := indexedResearch(database, sessionID, agentID, projectDir); researched || recorded {
			return researched
		}
	}
	// No tool_call events at all. On the hot-hook path this is ALWAYS the case,
	// so consult the canonical per-session tool log before conceding
	// (bug-a3b17225).
	if seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalResearchEvent); ok {
		debugLog(projectDir,
			"[wipnote] research-gate canonical fallback: session=%s research=%v", sessionID, seen)
		return seen
	}
	// Nothing canonical either — a genuine recording-pipeline gap (FK
	// failures, DB mismatch in worktrees, fresh session where only a
	// SessionStart event has been recorded). Fail open.
	debugLog(projectDir,
		"[wipnote] research-gate fail-open: no tool_call events recorded for session=%s agent=%s — recording pipeline may be broken",
		sessionID, agentID)
	return true
}

// indexedResearch asks the derived index whether research ran (researched) and
// whether it recorded any tool call at all (recorded). recorded=false is a
// recording gap, not a "no research" answer.
func indexedResearch(database *sql.DB, sessionID, agentID, projectDir string) (researched, recorded bool) {
	relatedSIDs := collectRelatedSessionIDs(database, sessionID)
	inClause, inArgs := buildInClause(relatedSIDs)
	useAgentID := agentID != "" && !isGenericAgentID(agentID)

	researchQuery, researchArgs := buildResearchQuery(inClause, inArgs, useAgentID, agentID, projectDir)
	var researchCount int
	database.QueryRow(researchQuery, researchArgs...).Scan(&researchCount)
	if researchCount > 0 {
		return true, true
	}
	toolCallQuery, toolCallArgs := buildToolCallQuery(inClause, inArgs, useAgentID, agentID, projectDir)
	var toolCallCount int
	database.QueryRow(toolCallQuery, toolCallArgs...).Scan(&toolCallCount)
	return false, toolCallCount > 0
}

// hasRecentWebResearch reports whether web/docs research — WebSearch/WebFetch
// (and harness equivalents) or a `gh ...` Bash command — ran in this session,
// an ancestor session, or by the same agent. It is the web-only subset of
// hasRecentResearch: local reads (Read/Grep/Glob/cat/head/…) are deliberately
// EXCLUDED, so an external-technology edit cannot be satisfied by reading a
// local file (feat-868c752b / spk-0a982f70).
//
// It mirrors hasRecentResearch's fail-open behavior: when the event-recording
// pipeline is broken (zero tool_call events across all related IDs) it returns
// true rather than false-blocking valid work.
func hasRecentWebResearch(database *sql.DB, sessionID, agentID, projectDir string) bool {
	if sessionID == "" {
		return true // fail-open: can't verify, don't block
	}
	if database != nil {
		if researched, recorded := indexedWebResearch(database, sessionID, agentID, projectDir); researched || recorded {
			return researched
		}
	}
	// No index (the hot-hook dispatch has none since feat-af8d1689): answer from
	// the canonical tool log, and fail open only when there is no log.
	if seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalWebResearchEvent); ok {
		return seen
	}
	debugLog(projectDir,
		"[wipnote] web-research-gate fail-open: no tool_call events recorded for session=%s agent=%s — recording pipeline may be broken",
		sessionID, agentID)
	return true
}

// indexedWebResearch is indexedResearch for the web-only research subset.
func indexedWebResearch(database *sql.DB, sessionID, agentID, projectDir string) (researched, recorded bool) {
	relatedSIDs := collectRelatedSessionIDs(database, sessionID)
	inClause, inArgs := buildInClause(relatedSIDs)
	useAgentID := agentID != "" && !isGenericAgentID(agentID)

	webQuery, webArgs := buildWebResearchQuery(inClause, inArgs, useAgentID, agentID, projectDir)
	var webCount int
	database.QueryRow(webQuery, webArgs...).Scan(&webCount)
	if webCount > 0 {
		return true, true
	}
	toolCallQuery, toolCallArgs := buildToolCallQuery(inClause, inArgs, useAgentID, agentID, projectDir)
	var toolCallCount int
	database.QueryRow(toolCallQuery, toolCallArgs...).Scan(&toolCallCount)
	return false, toolCallCount > 0
}

// buildInClause returns a SQL fragment like "(?, ?, ?)" and the matching args
// slice. If ids is empty the clause is "(NULL)" so the query is syntactically
// valid but matches nothing.
func buildInClause(ids []string) (string, []any) {
	if len(ids) == 0 {
		return "(NULL)", nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return "(" + strings.Join(placeholders, ", ") + ")", args
}

// researchShellToolNamesSQL is the SQL IN-list fragment for all harness shell
// tool names that can carry research reads: Bash (Claude Code), exec_command
// and functions.exec_command (Codex), run_shell_command (Gemini),
// run_command (Antigravity — Antigravity translates run_shell_command to
// run_command in its agent manifests). Used in both buildResearchQuery and
// buildWebResearchQuery so the two stay in lockstep (issue #144).
const researchShellToolNamesSQL = `'Bash', 'exec_command', 'functions.exec_command', 'run_shell_command', 'run_command'`

// buildResearchQuery builds the research detection SQL and its argument slice.
// It matches Read/Grep/Glob (and equivalents) plus web/docs/GitHub research
// (WebSearch/WebFetch and `gh ...`) under any of the related session IDs, and
// optionally also by agentID when useAgentID is true.
//
// The research-disposition source of truth lives in
// cmd/wipnote/prompts/research-routing.md and the agent-context skill
// (plugin/skills/agent-context/SKILL.md). Keep the qualifying-tool list here in
// sync so the guard does not penalize the web/docs/GitHub-first research those
// prompts encourage.
// When agentID is used, the query is scoped to events from the same project
// and from the last 24 hours to prevent cross-project and stale event leakage.
func buildResearchQuery(inClause string, inArgs []any, useAgentID bool, agentID, projectDir string) (string, []any) {
	sessionFilter, args := sessionOrAgentFilter(inClause, inArgs, useAgentID, agentID, projectDir)
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM agent_events
		WHERE %s
		  AND (
			tool_name IN (
				'Read', 'Grep', 'Glob',
				'WebSearch', 'WebFetch',
				'read_file', 'grep_search', 'glob', 'list_directory',
				'web_fetch', 'web_search', 'google_web_search'
			) OR (
				tool_name IN (`+researchShellToolNamesSQL+`) AND (
					input_summary LIKE 'ls %%' OR input_summary = 'ls'
					OR input_summary LIKE 'find %%'
					OR input_summary LIKE 'cat %%'
					OR input_summary LIKE 'grep %%'
					OR input_summary LIKE 'head %%'
					OR input_summary LIKE 'tail %%'
					OR input_summary LIKE 'stat %%'
					OR (input_summary LIKE 'sed %%' AND input_summary NOT LIKE 'sed -i%%' AND input_summary NOT LIKE 'sed --in-place%%')
					OR input_summary LIKE 'gh %%'
					OR input_summary LIKE 'curl %%'
					OR input_summary LIKE 'wipnote sh %%'
					OR input_summary LIKE 'wipnote search %%'
				)
			)
		  )
		LIMIT 1`, sessionFilter)
	return query, args
}

// buildWebResearchQuery builds the web/docs research detection SQL and its
// argument slice. It matches ONLY upstream-research tools — WebSearch/WebFetch
// (and the Codex/Gemini/Codex-browser equivalents) plus `gh ...` commands run
// through any supported shell tool — and deliberately EXCLUDES local reads
// (Read/Grep/Glob/cat/head/ls/find/…) so a dependency-manifest edit cannot be
// cleared by reading a local file (feat-868c752b).
//
// The web tool-name list is kept in lockstep with isOrchestratorResearchTool
// (which classifies the same tools as web/docs research), and the shell-tool
// list with isShellTool, so a session that researched via Codex's web.* tools or
// ran `gh search` through exec_command/functions.exec_command/run_shell_command
// is not falsely blocked (roborev #563/#566/#570, issue #144).
func buildWebResearchQuery(inClause string, inArgs []any, useAgentID bool, agentID, projectDir string) (string, []any) {
	sessionFilter, args := sessionOrAgentFilter(inClause, inArgs, useAgentID, agentID, projectDir)
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM agent_events
		WHERE %s
		  AND (
			tool_name IN (
				'WebSearch', 'WebFetch',
				'web_search', 'web_fetch', 'google_web_search',
				'web.search_query', 'web.open', 'web.find', 'web.click'
			) OR (
				tool_name IN (`+researchShellToolNamesSQL+`)
				AND input_summary LIKE 'gh %%'
			)
		  )
		LIMIT 1`, sessionFilter)
	return query, args
}

// buildToolCallQuery builds a query that counts all tool_call events (any tool)
// matching the session/agent filter. Used to distinguish a recording gap from
// a genuine no-research case.
// When agentID is used, the query is scoped to events from the same project
// and from the last 24 hours to prevent cross-project and stale event leakage.
func buildToolCallQuery(inClause string, inArgs []any, useAgentID bool, agentID, projectDir string) (string, []any) {
	sessionFilter, args := sessionOrAgentFilter(inClause, inArgs, useAgentID, agentID, projectDir)
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM agent_events
		WHERE %s
		  AND event_type = 'tool_call'
		LIMIT 1`, sessionFilter)
	return query, args
}

// sessionOrAgentFilter builds a SQL WHERE fragment that matches rows belonging
// to any of the related sessions OR (optionally) to the specific agentID.
// When agentID is used (i.e. useAgentID is true), additional scoping
// constraints are applied to ALL matched rows — including those matched via
// session_id IN (...):
//  1. Time window: only match events from the last 24 hours
//  2. Project scope (agent_id branch only): only match agent_id events whose
//     session_id belongs to the same project
//
// The 24h window is applied as an outer AND so that stale Reads in the
// current session don't bypass the freshness check just because the session
// happens to be long-running. The project scope is only enforced on the
// agent_id fallback branch — session lineage is project-authoritative.
func sessionOrAgentFilter(inClause string, inArgs []any, useAgentID bool, agentID, projectDir string) (string, []any) {
	if !useAgentID {
		return fmt.Sprintf("session_id IN %s", inClause), inArgs
	}

	// Build time window: 24 hours ago
	cutoffTime := time.Now().UTC().Add(-24 * time.Hour).Format(time.RFC3339)

	// Build the agent_id branch with project scoping. The 24h time window is
	// applied as an outer AND below so it constrains both branches.
	agentBranch := "(agent_id IS NOT NULL AND agent_id != '' AND agent_id = ?"
	args := append([]any{}, inArgs...)
	args = append(args, agentID)
	if projectDir != "" {
		normalizedDir := paths.NormalizeProjectDir(projectDir)
		agentBranch += " AND session_id IN (SELECT session_id FROM sessions WHERE project_dir = ?)"
		args = append(args, normalizedDir)
	}
	agentBranch += ")"

	filter := fmt.Sprintf(
		"((session_id IN %s OR %s) AND created_at > ?)",
		inClause, agentBranch,
	)
	args = append(args, cutoffTime)
	return filter, args
}

// getSessionAndParent returns the current session ID plus its parent session
// ID (if any). Worktree subagents inherit context from the outer orchestrator
// session that spawned them.
//
// Callers that need full transitive lineage should use collectRelatedSessionIDs
// instead. This function is preserved for existing callers (the diff-review,
// test-run and UI-validation gates) that only need one level of parent
// resolution. Claim inheritance no longer goes through here: see the canonical
// getClaimFromParentChain in canonical_context.go.
func getSessionAndParent(database *sql.DB, sessionID string) []string {
	sessionIDs := []string{sessionID}
	// Nil-DB-safe (roborev-478 finding 1): guard-only hook dispatch may pass a
	// nil DB; return just the current session (no parent walk) instead of
	// panicking on QueryRow.
	if database == nil {
		return sessionIDs
	}
	var parentID string
	database.QueryRow(
		`SELECT COALESCE(parent_session_id, '') FROM sessions WHERE session_id = ?`,
		sessionID,
	).Scan(&parentID)
	if parentID != "" {
		sessionIDs = append(sessionIDs, parentID)
	}
	return sessionIDs
}
