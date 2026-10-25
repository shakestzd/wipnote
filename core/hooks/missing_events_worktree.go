package hooks

import (
	"errors"
	"io"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/worktree"
)

// WorktreeCreate handles the WorktreeCreate Claude Code hook event.
//
// REPLACEMENT hook: Claude Code expects the hook process to create the
// worktree and print only the created directory path on stdout. Callers must
// bypass the JSON HookResult response layer.
func WorktreeCreate(event *CloudEvent) (string, error) {
	if event == nil {
		return "", errors.New("missing WorktreeCreate event")
	}
	if event.WorktreePath != "" {
		if event.WorktreeBasePath == "" {
			event.WorktreeBasePath = filepath.Dir(event.WorktreePath)
		}
		if event.WorktreeName == "" {
			event.WorktreeName = filepath.Base(event.WorktreePath)
		}
	}
	if event.WorktreeBasePath == "" && event.WorktreeName != "" && event.CWD != "" {
		// Claude Code payloads (observed 2026-06-11) carry only "name"; default
		// to the harness's conventional location under the project directory.
		event.WorktreeBasePath = filepath.Join(event.CWD, ".claude", "worktrees")
	}
	if event.WorktreeBasePath == "" {
		return "", errors.New("missing worktree_base_path")
	}
	if event.WorktreeName == "" {
		return "", errors.New("missing worktree_name")
	}

	worktreePath := event.WorktreePath
	if worktreePath == "" {
		worktreePath = filepath.Join(event.WorktreeBasePath, event.WorktreeName)
	}
	createdPath, err := worktree.CreateForClaudeHook(event.CWD, worktreePath, event.WorktreeName, io.Discard)
	if err != nil {
		return "", err
	}

	return createdPath, nil
}

// WorktreeRemove handles the WorktreeRemove Claude Code hook event.
//
// OBSERVATIONAL event: CC ignores this hook's stdout entirely (see
// hookEventContractSpecs). When a git worktree is removed it auto-completes any in-progress work items associated with the
// worktree branch.
//
// feat-cd937fc9 cleanup: a previous version returned an additionalContext
// message here. Because WorktreeRemove is Observational, CC discarded it — it
// never reached the agent. The dead return has been removed; the checkpoint
// branch auto-completion (the real side effect) is retained; it records
// nothing (feat-174dc958).
func WorktreeRemove(event *CloudEvent) (*HookResult, error) {
	// Auto-complete in-progress work items for the removed worktree's branch.
	// The branch name is typically the last path component of the worktree path
	// (e.g. /path/to/worktrees/trk-abc12345 → "trk-abc12345").
	branch := extractBranchFromWorktreePath(event.WorktreePath)
	if branch != "" {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		completedItems := autoCompleteByBranch(projectDir, branch)
		if len(completedItems) > 0 {
			debugLog(projectDir, "[worktree-remove] auto-completed %s (branch=%s)", strings.Join(completedItems, ", "), branch)
		}
	}
	return &HookResult{Continue: true}, nil
}

// extractBranchFromWorktreePath extracts the branch name from a worktree path.
// Claude Code typically names worktrees after the branch, so the last path
// component is the branch name (e.g. "/repo/.claude/worktrees/trk-abc12345").
func extractBranchFromWorktreePath(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	return filepath.Base(worktreePath)
}
