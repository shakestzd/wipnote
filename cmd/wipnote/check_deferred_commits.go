package main

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// failIfPendingDeferredArtifactCommits blocks the quality gate only on
// unresolved deferred artifact-commit intents that belong to workItemID, the
// item the current gate run is attributed to (bug-a5a846bc, #150). Intents
// belonging to OTHER, unrelated work items (typically old, already-completed
// items whose deferred commit was never flushed) no longer fail the gate —
// they are surfaced separately as a non-blocking repo-wide advisory via
// reportDeferredArtifactQueueHealth so the operator still sees the backlog
// without every later item inheriting a stranger's failure state.
func failIfPendingDeferredArtifactCommits(projectRoot, workItemID string, w io.Writer) error {
	ob, err := openCommitOutbox(projectRoot)
	if err != nil {
		return err
	}
	// GH#160: drain THIS item's own pending intents inline before deciding
	// whether anything still blocks — so `start` → `check --gate` passes
	// without a manual flush. Best-effort; whatever remains is judged below.
	flushWorkItemIntentsInline(ob, workItemID, w)
	pending, err := ob.Pending()
	if err != nil {
		return err
	}
	deadLettered, err := ob.DeadLettered()
	if err != nil {
		return err
	}

	var repoWidePending, repoWideDeadLettered int
	var pendingWorkItemIntents []commitqueue.Intent
	for _, intent := range pending {
		if !isWorkitemArtifactCommitIntent(intent) {
			continue
		}
		repoWidePending++
		if workItemScopedIntent(intent, workItemID) {
			pendingWorkItemIntents = append(pendingWorkItemIntents, intent)
		}
	}
	var deadLetteredWorkItemIntents []commitqueue.Intent
	for _, intent := range deadLettered {
		if !isWorkitemArtifactCommitIntent(intent) || deadLetteredArtifactIntentResolved(projectRoot, intent) {
			continue
		}
		repoWideDeadLettered++
		if workItemScopedIntent(intent, workItemID) {
			deadLetteredWorkItemIntents = append(deadLetteredWorkItemIntents, intent)
		}
	}

	reportDeferredArtifactQueueHealth(w, workItemID, repoWidePending, repoWideDeadLettered)

	workItemIntents := append([]commitqueue.Intent{}, pendingWorkItemIntents...)
	workItemIntents = append(workItemIntents, deadLetteredWorkItemIntents...)
	if len(workItemIntents) == 0 {
		return nil
	}
	var details []string
	for _, intent := range workItemIntents {
		if intent.WorkItemID != "" {
			details = append(details, intent.WorkItemID)
			continue
		}
		details = append(details, strings.Join(intent.RelPaths, ", "))
	}
	var remediation []string
	if len(pendingWorkItemIntents) > 0 {
		remediation = append(remediation, pendingIntentRemediation(projectRoot, pendingWorkItemIntents))
	}
	if len(deadLetteredWorkItemIntents) > 0 {
		remediation = append(remediation, "manually commit or revert the dead-lettered artifact changes, then clear the dead-letter entry")
	}
	return fmt.Errorf(
		"quality gate blocked by %d unresolved deferred work-item artifact commit intent(s): %s\nResolve: %s.",
		len(workItemIntents), strings.Join(details, ", "), strings.Join(remediation, "; "),
	)
}

// pendingIntentRemediation names the action that will actually unblock the
// gate. When a pending intent's artifact path is ignored by git, "run flush"
// is the one action that cannot work (GH#172) — so the gitignore explanation
// is printed instead.
func pendingIntentRemediation(projectRoot string, pending []commitqueue.Intent) string {
	for _, intent := range pending {
		if err := gitIgnoredIntentError(projectRoot, intent.RelPaths); err != nil {
			return "fix .gitignore first — " + err.Error()
		}
	}
	return "run `wipnote commit-queue flush` for pending intents"
}

func isWorkitemArtifactCommitIntent(intent commitqueue.Intent) bool {
	for _, rel := range intent.RelPaths {
		rel = normalizeIntentRelPath(rel)
		if strings.HasPrefix(rel, ".wipnote/features/") ||
			strings.HasPrefix(rel, ".wipnote/bugs/") ||
			strings.HasPrefix(rel, ".wipnote/spikes/") {
			return true
		}
	}
	return false
}

func deadLetteredArtifactIntentResolved(projectRoot string, intent commitqueue.Intent) bool {
	if !isGitRepo(projectRoot) {
		return false
	}
	for _, rel := range intent.RelPaths {
		rel = normalizeIntentRelPath(rel)
		out, err := exec.Command("git", "-C", projectRoot, "status", "--porcelain", "--", rel).Output()
		if err != nil || len(strings.TrimSpace(string(out))) > 0 {
			return false
		}
	}
	return true
}

func normalizeIntentRelPath(rel string) string {
	return strings.ReplaceAll(filepath.ToSlash(rel), "\\", "/")
}
