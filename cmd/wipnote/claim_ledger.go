package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// initClaimLedgerCommitSeam installs the commit producer for canonical claim
// ledger writes. core/claimledger cannot import internal/commitqueue (core must
// not depend on internal), but every writer — CLI command and hook handler
// alike — runs inside this binary, so wiring the seam once at startup covers
// them all.
func initClaimLedgerCommitSeam() {
	claimledger.OnCommit = persistClaimLedgerWrite
}

// persistClaimLedgerWrite records the claim ledger file for commit.
//
// COMMIT BATCHING: every episode mutation would otherwise be its own commit.
// The deferred artifact commit-queue already solves this and needed no changes:
// AppendCoalescingByRelPath drops older pending intents naming the same
// repo-relative path before appending the new one. One root session is one
// ledger file, so an entire session's claims and releases collapse into a
// SINGLE pending intent that commits the file's final state once, whenever
// `wipnote commit-queue flush` next drains.
//
// Under the legacy "separate" policy the write is committed directly, matching
// how work-item artifacts behave under that same opt-in.
func persistClaimLedgerWrite(wipnoteDir, relPath, action string) {
	msg := "wipnote: record claim " + action
	switch workitemArtifactCommitPolicyForEnv() {
	case workitemArtifactCommitPolicyDefer:
		if err := enqueueClaimLedgerCommitIntent(wipnoteDir, relPath, msg); err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "claim ledger commit defer: %v\n", err)
		}
	default:
		if err := commitWipnotePath(wipnoteDir, relPath, msg); err != nil && os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "claim ledger commit: %v\n", err)
		}
	}
}

func enqueueClaimLedgerCommitIntent(wipnoteDir, relPath, msg string) error {
	repoRoot := filepath.Dir(wipnoteDir)
	if skipWipnoteGitMutation(wipnoteDir, "claim ledger commit defer") {
		return nil
	}
	return recordCommitIntent(repoRoot, []string{relPath}, msg, "", "claim")
}

// claimLedgerStore returns the store for the project containing wipnoteDir.
func claimLedgerStore(wipnoteDir string) *claimledger.Store {
	return claimledger.NewStore(wipnoteDir)
}

// rootSessionFor resolves the ROOT session that owns sessionID — the shard key.
//
// Sharding by root rather than by agent keeps the file count to one per session
// instead of one per agent, and intra-session contention is handled by the write
// guard. Under Claude Code subagents already share the root's session ID, so the
// lookup is usually a no-op; it matters for harnesses that spawn subagents into
// their own sessions, where agent_lineage_trace carries the link.
//
// Falls back to sessionID whenever the lineage row is absent — a shard keyed by
// a session that turns out to be a child is still correct and still queryable,
// just slightly more granular than intended.
func rootSessionFor(database *sql.DB, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	if database == nil {
		return sessionID
	}
	var root string
	err := database.QueryRow(
		`SELECT COALESCE(root_session_id, '') FROM agent_lineage_trace
		 WHERE session_id = ? AND root_session_id != '' LIMIT 1`, sessionID,
	).Scan(&root)
	if err != nil || root == "" {
		return sessionID
	}
	return root
}

// recordClaimEpisodeOpen records the start of a claim episode when a work item
// is started. It is deliberately non-fatal: the claim ledger is observability,
// and a work item must never fail to start because history could not be written.
func recordClaimEpisodeOpen(database *sql.DB, wipnoteDir, sessionID, agentID, workItemID string) {
	if sessionID == "" || workItemID == "" {
		return
	}
	root := rootSessionFor(database, sessionID)
	store := claimLedgerStore(wipnoteDir)
	if _, _, err := store.Open(root, claimledger.Episode{
		WorkItemID:    workItemID,
		SessionID:     sessionID,
		RootSessionID: root,
		AgentID:       dbpkg.NormaliseAgentID(agentID),
		StartedAt:     time.Now().UTC(),
	}); err != nil {
		claimLedgerWarn("record claim start for %s: %v", workItemID, err)
	}
}

// recordClaimEpisodeClose records the end of a claim episode. A missing open
// episode is not an error — it just means the episode was already closed (a
// repeated complete, or a reconcile that got there first).
func recordClaimEpisodeClose(database *sql.DB, wipnoteDir, sessionID, agentID, workItemID string, outcome claimledger.Outcome) {
	if sessionID == "" || workItemID == "" {
		return
	}
	root := rootSessionFor(database, sessionID)
	store := claimLedgerStore(wipnoteDir)
	_, err := store.Close(root, sessionID, dbpkg.NormaliseAgentID(agentID), workItemID, outcome, time.Now().UTC())
	if err != nil && !errors.Is(err, claimledger.ErrNoOpenEpisode) {
		claimLedgerWarn("record claim end for %s: %v", workItemID, err)
	}
}

// sweepClaimsOnCompletion closes episodes the caller's exact-match close could
// not see (another session or agent, or completion with no session id). It must
// run only once the completion has stuck: the transactional and deferred commit
// paths reopen the item on failure, and sweeping before that would end another
// holder's claim on an item that is back in progress.
func sweepClaimsOnCompletion(wipnoteDir, workItemID, status string) {
	if status != "done" {
		return
	}
	recordWorkItemClaimsClosed(wipnoteDir, workItemID, claimledger.OutcomeCompleted)
}

// recordWorkItemClaimsClosed closes any episode still open on workItemID after
// the caller's own episode was closed. Completion belongs to the item, so a
// claim left by another session, another agent id, or no session at all must
// not outlive it. Non-fatal for the same reason as the other ledger writers.
func recordWorkItemClaimsClosed(wipnoteDir, workItemID string, outcome claimledger.Outcome) {
	if workItemID == "" {
		return
	}
	if _, err := claimLedgerStore(wipnoteDir).CloseWorkItem(workItemID, outcome, time.Now().UTC()); err != nil {
		claimLedgerWarn("close remaining claims for %s: %v", workItemID, err)
	}
}

// claimLedgerWarn reports a ledger failure without polluting stdout or breaking
// hooks (Claude Code treats any hook stderr as an error), so it is gated behind
// WIPNOTE_DEBUG like the other best-effort artifact paths.
func claimLedgerWarn(format string, args ...any) {
	if os.Getenv("WIPNOTE_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(stderr, "claim ledger: "+format+"\n", args...)
}

// claimLedgerLivePredicate reports whether a root session is still running,
// reusing the existing heartbeat-derived liveness signal rather than trusting
// sessions.status — a crashed session stays 'active' there forever, which is
// exactly the case reconcile has to catch.
func claimLedgerLivePredicate(database *sql.DB, projectDir string) claimledger.LivePredicate {
	if database == nil {
		return func(string) bool { return false }
	}
	window := dbpkg.LivenessStalenessThreshold(projectDir)
	current := hooks.EnvSessionID("")
	return func(rootSessionID string) bool {
		if rootSessionID == "" {
			// No identity to check — treat as live. Closing or archiving a session
			// that is actually running would corrupt a real interval, whereas
			// leaving it open just defers the repair to the next pass.
			return true
		}
		// Never reconcile or archive the session doing the reconciling.
		if current != "" && rootSessionID == current {
			return true
		}
		return dbpkg.SessionLivenessByHeartbeat(database, rootSessionID, window)
	}
}

// liveForeignClaims returns the open claim episodes on workItemID held by a
// session other than callerSessionID whose root session is still running
// (bug-ec1ff126). It reads the canonical claim ledger and sessions ledger
// directly rather than the derived projection, so `feature start` pays no
// hydrate and the answer cannot be stale.
//
// LIVENESS RULE: an episode is live while its root session has no EndedAt in
// the sessions ledger. The ledger carries no heartbeat, so a session that
// crashed without recording its end stays live until reconcile closes it; the
// refusal message points at --force for that case. A root session missing
// from the sessions ledger is treated as live, the conservative choice shared
// with claimLedgerLivePredicate: refusing costs a --force, while letting two
// live sessions both claim the item silently splits its attribution.
//
// ok is false when either ledger could not be read. Callers must treat that as
// "cannot verify" and fail open, per arch:unhydrated-table-guards-fail-open.
func liveForeignClaims(wipnoteDir, workItemID, callerSessionID string) (live []claimledger.Episode, ok bool) {
	episodes, err := claimledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return nil, false
	}
	var candidates []claimledger.Episode
	for _, e := range episodes {
		if !e.IsOpen() || e.WorkItemID != workItemID {
			continue
		}
		if callerSessionID != "" && (e.SessionID == callerSessionID || e.RootSessionID == callerSessionID) {
			continue // the caller's own family reclaiming is not a collision
		}
		candidates = append(candidates, e)
	}
	if len(candidates) == 0 {
		return nil, true
	}

	sessions, err := sessionledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return nil, false
	}
	ended := make(map[string]bool, len(sessions))
	for _, r := range sessions {
		ended[r.SessionID] = !r.IsOpen()
	}
	for _, e := range candidates {
		root := e.RootSessionID
		if root == "" {
			root = e.SessionID
		}
		if !ended[root] {
			live = append(live, e)
		}
	}
	return live, true
}

// checkLiveCollisionGate refuses to start workItemID while another live
// session holds it (feat-5a9839fb). --force bypasses the gate; an unreadable
// ledger fails open.
func checkLiveCollisionGate(wipnoteDir, workItemID, callerSessionID string) error {
	live, ok := liveForeignClaims(wipnoteDir, workItemID, callerSessionID)
	if !ok || len(live) == 0 {
		return nil
	}
	lines := make([]string, 0, len(live))
	for _, e := range live {
		lines = append(lines, fmt.Sprintf(
			"%s is actively claimed by session %s (agent %s, since %s).\n"+
				"Coordinate, or re-run with --force to claim anyway (e.g. if that session crashed).",
			workItemID, e.SessionID, e.AgentID, e.StartedAt.UTC().Format(time.RFC3339),
		))
	}
	return errors.New(strings.Join(lines, "\n"))
}
