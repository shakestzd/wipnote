package hooks

import (
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/paths"
)

// toolUseContext holds resolved identifiers shared by PreToolUse and PostToolUse.
type toolUseContext struct {
	SessionID       string
	FeatureID       string
	AgentID         string
	AgentType       string
	IsSubagent      bool
	ProjectDir      string
	HgDir           string
	IsYoloMode      bool
	ParentSessionID string // parent session ID for subagent context lookups
	ClaimedItem     string // work_item_id of agent's active claim, or ""
}

// resolveToolUseContext resolves session, agent identifiers, project
// directory and YOLO mode from a CloudEvent and canonical
// state. Returns nil when no session ID is found, indicating the caller should
// skip recording. FeatureID and ClaimedItem start empty: guards fill them from
// the canonical claim ledger (inheritParentChainClaim).
func resolveToolUseContext(event *CloudEvent) *toolUseContext {
	start := time.Now()

	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		return nil
	}

	agentID := resolveAgentID(event)
	isSubagent := isSubagentEvent(event)
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)

	// The parent session comes from durable state (session-family index /
	// WIPNOTE_PARENT_SESSION): checkSubagentCommitGuard reads "no parent" as
	// "this is the orchestrator — allow" (bug-2e5081b4 / GH-#88).
	parentSessionID := canonicalParentSessionID(projectDir, sessionID, isSubagent)

	agentType := event.AgentType
	if agentType == "" {
		agentType = os.Getenv("WIPNOTE_AGENT_TYPE")
	}

	hgDir := filepath.Join(projectDir, ".wipnote")
	yolo := isYoloWithInheritance(event, hgDir, sessionID, projectDir, isSubagent)

	LogTimed(projectDir, "pretooluse", map[string]string{
		"phase":   "resolve-context",
		"session": sessionID[:minSessionLen(sessionID)],
		"tool":    event.ToolName,
	}, start, "context resolved")

	return &toolUseContext{
		SessionID:       sessionID,
		AgentID:         agentID,
		AgentType:       agentType,
		IsSubagent:      isSubagent,
		ProjectDir:      projectDir,
		HgDir:           hgDir,
		IsYoloMode:      yolo,
		ParentSessionID: parentSessionID,
	}
}

// isSubagentEvent returns true when the event originates from a subagent.
// The listed values are the generic per-harness root/orchestrator markers —
// "" for Claude's own root case, and the harness-name constants each
// harness's parser falls back to when a payload carries no real per-subagent
// identity (see parseCodexEvent's default in harness.go). Any other value —
// a real per-subagent identity — means a subagent produced the event.
//
// bug-fa036758 / feat-b7bc4267: before feat-b7bc4267, parseCodexEvent
// unconditionally overwrote AgentID with the literal "codex" for every Codex
// event, subagent or not, so this function always returned false for Codex —
// no Codex event was ever detected as a subagent. Once parseCodexEvent
// started threading through the real payload agent_id when one is present,
// this function started correctly returning true for genuine Codex
// subagents, with NO code change needed here — the fix was entirely
// upstream, in what value AgentID carries.
//
// This is intended behavior, not an incidental side effect: it makes YOLO's
// subagent-scoped guards apply to Codex subagents the same way they already
// apply to Claude's (see the launcher comment at cmd/wipnote/codex.go's
// WIPNOTE_YOLO=1 injection — "so the worktree source-isolation guard fires
// under Codex just as it does under Claude Code's bypassPermissions path").
// Concretely: a Codex subagent under `wipnote codex --yolo` is now subject
// to checkSubagentWorkItemGuard (pretooluse.go — requires its OWN claimed
// work item, not just the orchestrator's session-wide one) and the
// worktree-isolation guard (checkYoloWorktreeGuard/checkYoloBashWorktreeGuard
// via the anyParentSessionYolo widening), where it previously was not.
// Both have a working path: feat-b7bc4267's PreToolUse rewrite lets a Codex
// subagent successfully claim a work item under its own identity, and
// WIPNOTE_YOLO=1 is already set on the whole launch environment, so a
// subagent's own yolo detection is normally already true — the widening
// only newly matters for a subagent whose own detection says no while its
// parent session is in yolo, which is exactly the gap it exists to close.
func isSubagentEvent(event *CloudEvent) bool {
	switch event.AgentID {
	case "", "claude-code", "claude", "codex", "gemini", "antigravity":
		return false
	default:
		return true
	}
}

// resolveAgentID returns the effective agent ID: the CloudEvent agent_id when
// present (subagent case), falling back to the per-subagent hint file
// (written by SubagentStart when CLAUDE_ENV_FILE is unset), then to the
// detected agent identity.
func resolveAgentID(event *CloudEvent) string {
	if event.AgentID != "" {
		return event.AgentID
	}
	// Check WIPNOTE_AGENT_ID env var (written to CLAUDE_ENV_FILE by SubagentStart
	// when CLAUDE_ENV_FILE is set).
	if id := os.Getenv("WIPNOTE_AGENT_ID"); id != "" {
		return id
	}
	// Fall back to the per-subagent hint file (written when CLAUDE_ENV_FILE is unset).
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID != "" {
		if hint := paths.ReadSubagentHint(sessionID); hint.AgentID != "" {
			return hint.AgentID
		}
	}
	return agent.Detect().ID
}

// resolveEventAgentID returns the agent ID from the CloudEvent, falling back
// to the detected agent identity. Use this for non-tooluse handlers
// (Stop, TrackEvent, etc.) that receive a raw CloudEvent.
func resolveEventAgentID(event *CloudEvent) string {
	if event.AgentID != "" {
		return event.AgentID
	}
	return agent.Detect().ID
}
