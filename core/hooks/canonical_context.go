package hooks

import (
	"os"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/paths"
)

// Canonical replacements for the tool-use facts the hot hooks used to read from
// the derived SQLite index.
//
// Since the feat-fc3cc9e0 cutover the PreToolUse hook opens an EMPTY in-memory
// projection (OpenHookDBReadOnly → db.OpenEphemeralProjectionTablesOnly), so
// every sessions/claims/agent_events read through that handle returns nothing.
// bug-369da005 repaired checkYoloWorkItemGuard by reading the canonical claim
// ledger; the helpers here give the sibling subagent guards the same treatment
// (bug-7036b94f / GH-#87, bug-2e5081b4 / GH-#88): the claim a subagent inherits
// from its orchestrator, the parent session it belongs to, and when it started
// are all derived from durable file state — the claim ledger, the session-family
// index, the per-subagent hint file — never from the projection.

// getClaimFromParentChain returns the work item a session inherits from an OPEN
// canonical claim episode held by itself or by its session-family root, plus
// the session that holds it. It is the canonical successor of the SQLite
// parent-chain walk that read sessions.parent_session_id + claims; that walk
// was structurally inert under the empty hook projection, which is exactly how
// a worktree subagent's Write was blocked with "claim=none" regardless of the
// orchestrator's `wipnote feature start` (GH-#87).
//
// Only consulted when the caller already has no claim of its own
// (claimedItem == ""): a session's own claim always wins and is passed through
// unchanged with an empty holder. Returns ("", "") when no open episode exists
// or the ledger cannot be read — the caller then falls through to the same
// block it would have produced before, with the ledger diagnostics intact.
//
// Claude Code subagents share the orchestrator's session ID, so the holder is
// frequently the SAME session ID as the caller: that is still an inherited
// claim from the caller's point of view (its own per-agent claim was empty).
func getClaimFromParentChain(wipnoteDir, sessionID, claimedItem string) (string, string) {
	if claimedItem != "" || wipnoteDir == "" || sessionID == "" {
		return claimedItem, ""
	}
	ep, ok := canonicalOpenClaimEpisode(wipnoteDir, sessionID)
	if !ok || ep.WorkItemID == "" {
		return "", ""
	}
	return ep.WorkItemID, ep.SessionID
}

// canonicalParentSessionID derives the parent session of sessionID from
// canonical state, replacing the sessions.parent_session_id column the hook
// projection no longer hydrates. resolveToolUseContext calls it to backfill
// toolUseContext.ParentSessionID (bug-2e5081b4), so every guard that gates on
// "has a parent" — the sub-agent commit guard above all — sees the real
// lineage instead of the empty projection's "".
//
// Resolution order:
//  1. WIPNOTE_PARENT_SESSION when it names a DIFFERENT session and the nesting
//     depth is not 0 (session-start exports the root's own ID at depth 0, which
//     is not a parent).
//  2. For a subagent, the session-family root when it is a distinct session
//     (Codex-style subagents own their session ID and are registered under
//     the launcher's family).
//  3. For a subagent whose session ID IS the family root — Claude Code, where
//     subagents share the orchestrator's session ID — the orchestrator session
//     is that same ID. Reporting it keeps "has a parent" true for the guards
//     that gate on it (the sub-agent commit guard, the grace period).
//
// A non-subagent root session has no parent: a resumed continuation may share
// a family with an earlier root, but it is an orchestrator in its own right and
// must keep the orchestrator's permissions (e.g. committing on main).
func canonicalParentSessionID(projectDir, sessionID string, isSubagent bool) string {
	if sessionID == "" {
		return ""
	}
	if env := os.Getenv("WIPNOTE_PARENT_SESSION"); env != "" && env != sessionID &&
		os.Getenv("WIPNOTE_NESTING_DEPTH") != "0" {
		return env
	}
	if !isSubagent {
		return ""
	}
	if root := agent.SessionFamilyFor(projectDir, sessionID); root != "" && root != sessionID {
		return root
	}
	return sessionID
}

// subagentStartedAt is the start time for ctx's grace window: the
// projection-supplied sessions.created_at when a hydrated row provided one,
// else the canonical derivation for the acting agent.
func subagentStartedAt(ctx *toolUseContext) time.Time {
	if !ctx.SessionCreatedAt.IsZero() {
		return ctx.SessionCreatedAt
	}
	return canonicalSubagentStartedAt(ctx.ProjectDir, ctx.SessionID, ctx.AgentID)
}

// canonicalSubagentStartedAt reports when the acting subagent started, for the
// grace window that lets a freshly spawned subagent write before it has claimed.
// The projection's sessions.created_at is gone with the read index; the
// per-subagent hint file written by SubagentStart carries the same fact for the
// specific agent, and the per-session state file (written when a subagent owns
// its session ID) is the fallback. Zero when neither records this agent.
func canonicalSubagentStartedAt(projectDir, sessionID, agentID string) time.Time {
	if sessionID == "" {
		return time.Time{}
	}
	if hint := paths.ReadSubagentHint(sessionID); hint.AgentID != "" && hint.AgentID == agentID && !hint.StartedAt.IsZero() {
		return hint.StartedAt
	}
	if st, err := agent.ReadSessionState(projectDir, sessionID); err == nil && st != nil && st.Timestamp > 0 {
		return time.Unix(st.Timestamp, 0).UTC()
	}
	return time.Time{}
}
