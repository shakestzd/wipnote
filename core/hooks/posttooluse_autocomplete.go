package hooks

import (
	"database/sql"
	"os/exec"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/db"
)

// commitClosingRe matches closing keywords followed by a work item ID in commit messages.
// Supports: "completes feat-abc123", "closes bug-def456", "fixes spk-789abc",
// "resolves feat-abc123", and parenthetical form "(feat-abc123)".
// Case-insensitive matching is applied at call site via strings.ToLower.
var commitClosingRe = regexp.MustCompile(`(?:completes?|closes?|fix(?:es)?|resolves?)\s+((?:feat|bug|spk)-[0-9a-f]{8})`)

// commitParenRe matches parenthetical work item references at the end of commit
// messages, e.g. "(feat-abc12345)" or "(bug-900f6655, #114)".
// Allows trailing tokens (e.g. issue numbers) inside the parens via [^)]*.
var commitParenRe = regexp.MustCompile(`\(\s*((?:feat|bug|spk)-[0-9a-f]{8})\b[^)]*\)`)

// extractClosingIDs parses a commit message for work item IDs that should be
// auto-completed. It recognises two patterns:
//  1. Closing keywords: "completes feat-abc123", "fixes bug-def456"
//  2. Parenthetical refs: "(feat-abc123)" — the existing wipnote convention
//
// Returns a deduplicated slice of work item IDs.
func extractClosingIDs(commitMsg string) []string {
	seen := map[string]bool{}
	var ids []string

	lower := strings.ToLower(commitMsg)
	for _, m := range commitClosingRe.FindAllStringSubmatch(lower, -1) {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, m := range commitParenRe.FindAllStringSubmatch(lower, -1) {
		id := m[1]
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// autoCompleteFromCommit auto-completes work items referenced in a git commit
// message. It handles two modes:
//
//  1. Keyword/parenthetical mode (all sessions): when commit message contains
//     closing keywords or parenthetical work item refs, complete those items.
//  2. YOLO mode: also complete the session's active feature on any commit,
//     even without explicit references. When the current session has no
//     FeatureID (e.g. a worktree subagent whose orchestrator started the work
//     item), falls back to the parent session's active feature.
//
// Uses the selfBinary() + exec.Command() pattern to avoid importing workitem.
func autoCompleteFromCommit(commitMsg string, ctx *toolUseContext, database *sql.DB) []string {
	var completed []string

	// Mode 1: keyword/parenthetical references — works in all sessions.
	ids := extractClosingIDs(commitMsg)
	for _, id := range ids {
		if completeIfInProgress(id, database) {
			completed = append(completed, id)
		}
	}

	// Mode 2: YOLO auto-complete of active feature (no keywords needed).
	if ctx.IsYoloMode {
		featureID := ctx.FeatureID
		// Fallback: worktree subagents may have no FeatureID of their own when
		// the orchestrator's session started the work item. Check the parent.
		if featureID == "" && ctx.ParentSessionID != "" {
			featureID = db.GetActiveFeatureIDForSession(database, ctx.ParentSessionID)
		}
		if featureID != "" {
			// Don't double-complete if already handled above.
			alreadyDone := false
			for _, id := range completed {
				if id == featureID {
					alreadyDone = true
					break
				}
			}
			if !alreadyDone {
				if completeIfInProgress(featureID, database) {
					completed = append(completed, featureID)
				}
			}
		}
	}

	return completed
}

// completeIfInProgressFn is the implementation used by completeIfInProgress.
// It is a package-level variable so tests can inject a stub to avoid shelling
// out to the CLI binary (which requires a real on-disk DB at a known path).
var completeIfInProgressFn = completeIfInProgressImpl

// completeIfInProgress checks whether a work item is in-progress and, if so,
// shells out to the CLI to complete it. Returns true if completion was triggered.
func completeIfInProgress(id string, database *sql.DB) bool {
	return completeIfInProgressFn(id, database)
}

// completeIfInProgressImpl is the real implementation of completeIfInProgress.
//
// The status read goes through LookupWorkItem (feat-f6759e37): under a
// launcher-guaranteed session it is answered from canonical state by the
// daemon, and under no guarantee from the derived index exactly as before. A
// daemon failure under guarantee returns not-found here AND latches a breach,
// so this returns false without acting — and the hook's response is replaced
// with a loud block before that false is mistaken for "nothing to complete".
func completeIfInProgressImpl(id string, database *sql.DB) bool {
	item, found := LookupWorkItem(database, id)
	if !found || item.Status != "in-progress" {
		return false
	}
	typeName := inferTypeName(id)
	cmd := exec.Command(selfBinary(), typeName, "complete", id)
	if err := cmd.Run(); err != nil {
		debugLog("", "[posttooluse] auto-complete failed for %s: %v", id, err)
		return false
	}
	debugLog("", "[posttooluse] auto-completed %s", id)
	return true
}

// gitCommitOutputRe matches the commit line from git commit output, e.g.:
// "[main abc1234] commit message here"
var gitCommitOutputRe = regexp.MustCompile(`\[[\w/\-]+\s+([0-9a-f]{7,40})\]\s+(.*)`)

// looksLikeGitCommit returns true when the bash command appears to be a git commit.
func looksLikeGitCommit(cmd string) bool {
	return strings.Contains(cmd, "git commit") || strings.Contains(cmd, "git-commit")
}

// looksLikeGitMerge returns true when the bash command appears to be a git merge.
func looksLikeGitMerge(cmd string) bool {
	return strings.Contains(cmd, "git merge") || strings.Contains(cmd, "git-merge")
}

// gitMergeBranchRe matches the branch name in a git merge command.
// Handles: "git merge trk-abc123", "git merge --no-ff feat-abc123", etc.
var gitMergeBranchRe = regexp.MustCompile(`git[-\s]merge\s+(?:--\S+\s+)*(\S+)`)

// extractMergeBranch parses a git merge command and returns the branch being merged.
// Returns "" when the branch cannot be determined.
func extractMergeBranch(cmd string) string {
	// Find the last argument after flags (skip option flags starting with -)
	for _, line := range strings.Split(cmd, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "git merge") && !strings.Contains(line, "git-merge") {
			continue
		}
		if m := gitMergeBranchRe.FindStringSubmatch(line); len(m) == 2 {
			branch := m[1]
			// Filter out common flags that look like arguments
			if strings.HasPrefix(branch, "-") {
				return ""
			}
			return branch
		}
	}
	return ""
}

// workItemBranchRe matches a branch name that is itself a work item ID:
// feat-xxxxxxxx, bug-xxxxxxxx, spk-xxxxxxxx (8 hex chars).
var workItemBranchRe = regexp.MustCompile(`^((?:feat|bug|spk)-[0-9a-f]{8})$`)

// trackBranchRe matches a branch that is a track ID: trk-xxxxxxxx.
var trackBranchRe = regexp.MustCompile(`^(trk-[0-9a-f]{8})$`)

// autoCompleteByBranch completes in-progress work items based on a branch name.
// When the branch is a track ID (trk-xxxxxxxx), all in-progress features/bugs/spikes
// on that track are completed. When it is a direct work item ID, only that item
// is completed. Returns the IDs of completed items.
func autoCompleteByBranch(branch string, database *sql.DB) []string {
	// Direct work item branch: feat-xxxxxxxx, bug-xxxxxxxx, spk-xxxxxxxx
	if workItemBranchRe.MatchString(branch) {
		if completeIfInProgress(branch, database) {
			return []string{branch}
		}
		return nil
	}

	// Track branch: trk-xxxxxxxx — complete all in-progress items on this track
	if m := trackBranchRe.FindStringSubmatch(branch); len(m) == 2 {
		trackID := m[1]
		return completeInProgressByTrack(trackID, database)
	}

	return nil
}

// completeInProgressByTrack queries for all in-progress work items on a track
// and shells out to complete each one. Returns the IDs of completed items.
//
// The listing goes through ListWorkItems (feat-f6759e37) — canonical state via
// the daemon under a launcher guarantee, the derived index otherwise.
func completeInProgressByTrack(trackID string, database *sql.DB) []string {
	items := ListWorkItems(database, daemon.WorkItemListArgs{
		TrackID:  trackID,
		Statuses: []string{"in-progress"},
	})
	var ids []string
	for _, it := range items {
		ids = append(ids, it.ID)
	}

	var completed []string
	for _, id := range ids {
		if completeIfInProgress(id, database) {
			completed = append(completed, id)
		}
	}
	return completed
}

// parseGitCommitOutput extracts the commit hash and message from git's stdout.
// Returns ("", "") when the output does not match the expected format.
func parseGitCommitOutput(output string) (hash, message string) {
	for _, line := range strings.Split(output, "\n") {
		if m := gitCommitOutputRe.FindStringSubmatch(strings.TrimSpace(line)); len(m) == 3 {
			return m[1], strings.TrimSpace(m[2])
		}
	}
	return "", ""
}
