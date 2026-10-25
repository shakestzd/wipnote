package hooks

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
)

// checkYoloWorkItemGuard blocks Write/Edit tools when no active work item
// exists. Always enforced (was YOLO-only, now universal).
//
// featureID is the session's active_feature_id column (set at session-start
// or inherited from a parent session via lineage).
// sessionID is used for the fallback check: when featureID is empty, we check
// whether a feature was started mid-session and linked to THIS session — not
// whether any feature is globally in-progress (which causes false passes when
// unrelated features exist).
// targetFile is the Write/Edit target path extracted from the tool input.
// When the target is outside the project root (home config, /tmp, sibling repos,
// etc.) the guard is skipped — project-code discipline must not constrain
// writes to external locations like ~/.claude/ memory files.
// projectRoot is the resolved project directory (ctx.ProjectDir); when empty
// the guard applies unconditionally (conservative).
func checkYoloWorkItemGuard(toolName, featureID string, _ bool, sessionID string, database *sql.DB, targetFile, projectRoot string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	// Skip for writes targeting paths outside the project root.
	if targetFile != "" && pathIsOutsideProject(targetFile, projectRoot) {
		return ""
	}
	// Check 1: this session's own active work item (set at session-start or via
	// lineage attribution promoted into featureID at the call site).
	if featureID != "" {
		return ""
	}
	// Check 2: a feature started mid-session and linked to THIS exact session row.
	if sessionID != "" && database != nil && sessionHasLinkedFeature(database, sessionID) {
		return ""
	}
	// Check 3: parent-chain fallback for nested/subagent sessions. A subagent's
	// `wipnote bug start` writes active_feature_id to the orchestrator's session
	// row, not the subagent's own row, so checks 1-2 miss even though the work is
	// legitimately attributed. One hop only (getSessionAndParent) keeps this tied
	// to direct lineage and avoids the global "any in-progress item" false-pass.
	if sessionID != "" && database != nil && ancestorHasActiveWorkItem(database, sessionID) {
		return ""
	}
	// Check 4: the CANONICAL claim ledger. Checks 1-3 all read the derived
	// SQLite index, which the hook read path no longer hydrates (bug-369da005) —
	// without this they are structurally incapable of returning true and the
	// guard degrades into a universal write block. See canonicalOpenClaim.
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
	return workItemRequiredMessage(sessionID, database, projectRoot)
}

// workItemRequiredMessage is checkYoloWorkItemGuard's block reason, naming the
// checked session and the nearest active claim when known.
func workItemRequiredMessage(sessionID string, database *sql.DB, projectRoot string) string {
	msg := "An active work item is required before writing code. " +
		"Run: wipnote feature start <id>  or  wipnote feature create \"title\" --track <trk-id>"
	if sessionID != "" {
		msg += fmt.Sprintf("\nChecked session: %s", sessionID)
	}
	if database != nil {
		if claimSession, workItem := latestProjectClaim(database, projectRoot); claimSession != "" && workItem != "" {
			msg += fmt.Sprintf("\nNearest active claim: session=%s work_item=%s", claimSession, workItem)
		}
	}
	return msg
}

// canonicalOpenClaim reports the work item held by an OPEN claim episode for
// sessionID, read from the CANONICAL claim ledger (.wipnote/claims/) rather
// than the derived SQLite index.
//
// WHY THIS EXISTS. Checks 1-3 of checkYoloWorkItemGuard all query the derived
// index. Since the feat-fc3cc9e0 cutover the hook read path has opened that
// index as a per-process in-memory projection it never hydrates (and since
// feat-af8d1689 gets no handle at all), so those three checks can only ever
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
			e := episodes[i]
			if !e.IsOpen() || e.WorkItemID == "" {
				continue
			}
			// Either this session holds the claim itself, or the family root
			// does and this session is a descendant inheriting it — the
			// canonical mirror of check 3's parent-chain fallback.
			if e.SessionID == sessionID || e.SessionID == root {
				return e, true
			}
		}
	}
	return claimledger.Episode{}, consulted
}

func latestProjectClaim(database *sql.DB, projectRoot string) (string, string) {
	if database == nil || projectRoot == "" {
		return "", ""
	}
	var sessionID, workItemID string
	err := database.QueryRow(`
		SELECT c.owner_session_id, c.work_item_id
		FROM claims c
		JOIN sessions s ON s.session_id = c.owner_session_id
		WHERE s.status = 'active'
		  AND COALESCE(s.project_dir, '') = ?
		  AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
		ORDER BY c.leased_at DESC
		LIMIT 1`, projectRoot).Scan(&sessionID, &workItemID)
	if err != nil {
		return "", ""
	}
	return sessionID, workItemID
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

// sessionHasLinkedFeature returns true when the given session has a feature
// linked via sessions.active_feature_id OR when a recent feature-start command
// updated the session's feature association. This replaces the old global
// hasAnyInProgressWorkItem check which false-passed when unrelated features
// were in-progress elsewhere in the project.
func sessionHasLinkedFeature(db *sql.DB, sessionID string) bool {
	var featureID sql.NullString
	db.QueryRow(
		`SELECT active_feature_id FROM sessions WHERE session_id = ? LIMIT 1`,
		sessionID,
	).Scan(&featureID)
	return featureID.Valid && featureID.String != ""
}

// ancestorHasActiveWorkItem returns true when sessionID's direct parent session
// has a non-empty active_feature_id pointing at an in-progress feature. This
// lets a nested/subagent session inherit the orchestrator's work-item context:
// subagents don't write their own sessions.active_feature_id, so a legitimately
// attributed edit would otherwise be false-blocked.
//
// One hop only (getSessionAndParent) — tied to direct lineage so it does NOT
// reintroduce the global "any in-progress work item" false-pass that the removed
// hasAnyActiveWorkItem caused. The JOIN to features enforces status =
// 'in-progress', matching GetToolUseContext semantics so a stale pointer to a
// completed feature does not pass. Returns false on nil DB / empty sessionID /
// no parent.
func ancestorHasActiveWorkItem(database *sql.DB, sessionID string) bool {
	if database == nil || sessionID == "" {
		return false
	}
	sessionIDs := getSessionAndParent(database, sessionID)
	if len(sessionIDs) < 2 {
		return false // no parent
	}
	for _, parentID := range sessionIDs[1:] {
		var count int
		database.QueryRow(`
			SELECT COUNT(*) FROM sessions s
			JOIN features f ON f.id = s.active_feature_id
			WHERE s.session_id = ?
			  AND f.status = 'in-progress'
			LIMIT 1`, parentID).Scan(&count)
		if count > 0 {
			return true
		}
	}
	return false
}
