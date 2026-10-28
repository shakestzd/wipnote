package hooks

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
)

// checkYoloWorkItemGuard blocks Write/Edit tools when no active work item
// exists. Always enforced (was YOLO-only, now universal).
//
// featureID is the session's active work item when the caller already knows
// it. sessionID is used for the fallback check: when featureID is empty, the
// canonical claim ledger says whether THIS session (or its family root) holds
// an open claim — not whether any feature is globally in-progress (which
// causes false passes when unrelated features exist).
// targetFile is the Write/Edit target path extracted from the tool input.
// When the target is outside the project root (home config, /tmp, sibling repos,
// etc.) the guard is skipped — project-code discipline must not constrain
// writes to external locations like ~/.claude/ memory files.
// projectRoot is the resolved project directory (ctx.ProjectDir); when empty
// the guard applies unconditionally (conservative).
func checkYoloWorkItemGuard(toolName, featureID string, _ bool, sessionID string, targetFile, projectRoot string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	// Skip for writes targeting paths outside the project root.
	if targetFile != "" && pathIsOutsideProject(targetFile, projectRoot) {
		return ""
	}
	// Check 1: this session's own active work item, when the caller has it.
	if featureID != "" {
		return ""
	}
	// Check 2: the CANONICAL claim ledger, which covers a feature started
	// mid-session and a subagent inheriting its family root's claim. The hook
	// has no derived index to ask (bug-369da005, feat-174dc958).
	//
	// ok=false means the ledger could not be consulted at all, which is
	// "cannot verify", not "no claim" — fail OPEN there, matching the
	// fail-open posture the sibling research guard already had.
	if sessionID != "" && projectRoot != "" {
		workItem, ok := canonicalOpenClaim(filepath.Join(projectRoot, ".wipnote"), sessionID)
		if !ok || workItem != "" {
			return ""
		}
	}
	return workItemRequiredMessage(sessionID)
}

// workItemRequiredMessage is checkYoloWorkItemGuard's block reason, naming the
// checked session when known.
func workItemRequiredMessage(sessionID string) string {
	msg := "An active work item is required before writing code. " +
		"Run: wipnote feature start <id>  or  wipnote feature create \"title\" --track <trk-id>"
	if sessionID != "" {
		msg += fmt.Sprintf("\nChecked session: %s", sessionID)
	}
	return msg
}

// canonicalOpenClaim reports the work item held by an OPEN claim episode for
// sessionID, read from the CANONICAL claim ledger (.wipnote/claims/) rather
// than the derived SQLite index.
//
// WHY THIS EXISTS. checkYoloWorkItemGuard's fallbacks used to query the
// derived index. Since the feat-fc3cc9e0 cutover the hook read path has opened
// that index as a per-process in-memory projection it never hydrates (and
// since feat-af8d1689 gets no handle at all), so those checks could only ever
// return false. Because the guard fails CLOSED, that turned it into a universal
// Write/Edit block for every session on the machine the moment a post-cutover
// binary shipped (bug-369da005). The sibling research guard survived the same
// cutover only because it fails OPEN and so merely went inert.
//
// The claim ledger is the canonical record that `wipnote feature start` writes,
// so consulting it restores what this guard was actually built to assert
// instead of deleting the guard. This mirrors rootSessionForClaimLedger, which
// replaced an equivalent dead agent_lineage_trace lookup for the same reason.
//
// COST. Two direct shard reads, never ReadAll: shards are keyed by ROOT
// session, so a Claude Code session (whose subagents share the root's session
// ID) resolves on the first read, and a Codex subagent — which owns a distinct
// session ID but is recorded inside the root's shard — resolves on the
// family-root read. That keeps the work bounded per tool call, which matters
// because this runs on every Write/Edit.
//
// RETURN CONTRACT. ok reports whether the ledger was successfully consulted.
// A MISSING shard is a successful read of zero episodes (readShard maps
// os.IsNotExist to nil, nil) and therefore authoritative "no claim" — the guard
// must still block there, which is the case it exists to catch. Only a genuine
// read error yields ok=false, which callers must treat as "cannot verify" and
// fail open.
func canonicalOpenClaim(wipnoteDir, sessionID string) (workItem string, ok bool) {
	ep, ok := canonicalOpenClaimEpisode(wipnoteDir, sessionID)
	return ep.WorkItemID, ok
}

// canonicalOpenClaimEpisode is canonicalOpenClaim returning the whole episode,
// so callers that need to know WHICH session holds the inherited claim (the
// subagent guards in pretooluse.go, see getClaimFromParentChain) can report it.
// The zero Episode with ok=true means "ledger consulted, no open claim".
func canonicalOpenClaimEpisode(wipnoteDir, sessionID string) (ep claimledger.Episode, ok bool) {
	// Either this session holds the claim itself, or the family root does and
	// this session is a descendant inheriting it.
	return scanOpenClaims(wipnoteDir, sessionID, func(e claimledger.Episode, root string) bool {
		return e.SessionID == sessionID || e.SessionID == root
	})
}

// scanOpenClaims returns the newest open claim in the shards of sessionID and
// its family root that match accepts (called with the shard's root). The zero
// Episode with ok=true means "ledger consulted, no match".
func scanOpenClaims(wipnoteDir, sessionID string, accepts func(e claimledger.Episode, root string) bool) (claimledger.Episode, bool) {
	if wipnoteDir == "" || sessionID == "" {
		return claimledger.Episode{}, false
	}
	roots := []string{sessionID}
	// Only distinct when sessionID is a descendant; yoloFamilyRoot returns ""
	// for a session that is its own family root.
	if root := yoloFamilyRoot(wipnoteDir, sessionID); root != "" {
		roots = append(roots, root)
	}

	store := claimledger.NewStore(wipnoteDir)
	consulted := false
	for _, root := range roots {
		episodes, err := store.ReadShard(root)
		if err != nil {
			continue // unreadable shard: cannot verify via this one
		}
		consulted = true
		// Newest-first: a session that re-claimed should resolve to its most
		// recent open episode.
		for i := len(episodes) - 1; i >= 0; i-- {
			if e := episodes[i]; e.IsOpen() && e.WorkItemID != "" && accepts(e, root) {
				return e, true
			}
		}
	}
	return claimledger.Episode{}, consulted
}

// yoloSubagentGracePeriod is the window after session start during which a
// subagent is allowed to write files before claiming a work item. This gives
// the subagent time to run `wipnote feature start <id>` as its first action.
const yoloSubagentGracePeriod = 30 * time.Second

// checkYoloSubagentGrace returns true when the session qualifies for the
// subagent grace period: it must be a subagent (nesting_depth > 0 per
// is_subagent flag), the subagent must have started less than
// yoloSubagentGracePeriod ago, and the parent session must hold an OPEN claim
// in the canonical claim ledger. When these conditions hold the caller should
// allow the write with a warning instead of blocking.
//
// startedAt and parentSessionID come from canonical state (see
// canonicalSubagentStartedAt / canonicalParentSessionID): the sessions row the
// hook projection used to supply both from is never hydrated any more, which
// left this window permanently closed (bug-7036b94f). A zero startedAt means
// the start could not be established and is treated as "outside the window"
// — the guard stays ON rather than being skipped on missing evidence.
func checkYoloSubagentGrace(yolo, isSubagent bool, startedAt time.Time, parentSessionID, wipnoteDir string) bool {
	if !yolo || !isSubagent {
		return false
	}
	if startedAt.IsZero() || time.Since(startedAt) >= yoloSubagentGracePeriod {
		return false
	}
	if parentSessionID == "" || wipnoteDir == "" {
		return false
	}
	workItem, _ := canonicalOpenClaim(wipnoteDir, parentSessionID)
	return workItem != ""
}
