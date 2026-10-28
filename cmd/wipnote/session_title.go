package main

import (
	"os"
	"path/filepath"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// markSessionTitle asks the next UserPromptSubmit hook to rename the claiming
// session "<item-id> <title>" (plan-28b4a7f6 slice 15). Only a root Claude
// session is renamed: a subagent's claim belongs to its parent, a Codex
// session has no documented title output, and a delegate worker keeps the
// name its mailbox and herdr agent use. Best effort: a failed mark never
// fails the start.
func markSessionTitle(wipnoteDir, sessionID, agentID, itemID string) {
	if dbpkg.NormaliseAgentID(agentID) != dbpkg.AgentRootSentinel || os.Getenv(session.RunEnv) != "" {
		return
	}
	_ = hooks.MarkSessionTitlePending(filepath.Dir(wipnoteDir), sessionID, itemID)
}
