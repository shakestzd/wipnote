package hooks

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/worktree"
)

// worktreePathResolver is the resolver function passed to
// paths.NormalizeWithResolver for worktree path normalization.  It receives
// a directory and must return the repo root anchor for that directory, or ""
// when the directory is not inside any wipnote-aware git repo.
//
// Production value: paths.ResolveWipnoteAnchorForDir (git-based resolver).
// Tests override this var with a stub that returns a synthetic repo root
// without shelling to git.
var worktreePathResolver = paths.ResolveWipnoteAnchorForDir

// normalizeWorktreePath converts an absolute WorktreePath to a
// repo-relative, forward-slash form using the paths.NormalizeWithResolver
// contract.  Already-relative paths pass through unchanged.  Foreign paths
// (outside any wipnote repo) receive the "unresolved:" prefix.  Empty input
// is returned as-is (no-op / no panic).
//
// The resolver is invoked directly on filepath.Dir(worktreePath) so that
// worktrees that have already been removed from disk (WorktreeRemove case)
// can still be normalized — we never need the path to exist on disk.
func normalizeWorktreePath(worktreePath string) string {
	if worktreePath == "" {
		return ""
	}
	if !filepath.IsAbs(worktreePath) {
		return worktreePath
	}
	// Resolve the repo root from the parent directory of the worktree path.
	// We call the resolver explicitly rather than relying on NormalizeWithResolver's
	// internal nearestExistingDir walk so that already-removed worktree directories
	// (not on disk) are still normalized correctly.
	repoRoot := worktreePathResolver(filepath.Dir(worktreePath))
	norm, _ := paths.NormalizeWithResolver(worktreePath, repoRoot, worktreePathResolver)
	return norm
}

// WorktreeCreate handles the WorktreeCreate Claude Code hook event.
//
// REPLACEMENT hook: Claude Code expects the hook process to create the
// worktree and print only the created directory path on stdout. Callers must
// bypass the JSON HookResult response layer.
func WorktreeCreate(event *CloudEvent, database *sql.DB) (string, error) {
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

	// Checkpoint recording is best-effort and canonical-first: a nil database
	// means the derived-index open was unavailable (writer_unavailable), but
	// the worktree itself was created successfully, so we MUST still return the
	// bare path (#119 stdout contract). Skip the derived-index write when the
	// handle is nil; reindex recovers it on the next serve cycle.
	if database != nil {
		summary := fmt.Sprintf("Worktree created: %s", normalizeWorktreePath(createdPath))
		if _, err := recordSimpleEvent(models.EventCheckPoint, "WorktreeCreate", summary, "recorded", event, database); err != nil {
			projectDir := ResolveProjectDir(event.CWD, event.SessionID)
			debugLog(projectDir, "[worktree-create] checkpoint record failed: %v", err)
		}
	}

	return createdPath, nil
}

// WorktreeRemove handles the WorktreeRemove Claude Code hook event.
//
// OBSERVATIONAL event: CC ignores this hook's stdout entirely (see
// hookEventContractSpecs). Records when a git worktree is removed after work is
// complete and auto-completes any in-progress work items associated with the
// worktree branch.
//
// feat-cd937fc9 cleanup: a previous version returned an additionalContext
// message here. Because WorktreeRemove is Observational, CC discarded it — it
// never reached the agent. The dead return has been removed; the checkpoint
// recording and branch auto-completion (the real side effects) are retained.
func WorktreeRemove(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	summary := "Worktree removed"
	if event.WorktreePath != "" {
		normPath := normalizeWorktreePath(event.WorktreePath)
		summary = fmt.Sprintf("Worktree removed: %s", normPath)
	}

	result, err := recordSimpleEvent(models.EventCheckPoint, "WorktreeRemove", summary, "recorded", event, database)
	if err != nil || result == nil {
		return result, err
	}

	// Auto-complete in-progress work items for the removed worktree's branch.
	// The branch name is typically the last path component of the worktree path
	// (e.g. /path/to/worktrees/trk-abc12345 → "trk-abc12345").
	branch := extractBranchFromWorktreePath(event.WorktreePath)
	if branch != "" {
		completedItems := autoCompleteByBranch(branch, database)
		if len(completedItems) > 0 {
			projectDir := ResolveProjectDir(event.CWD, event.SessionID)
			debugLog(projectDir, "[worktree-remove] auto-completed %s (branch=%s)", strings.Join(completedItems, ", "), branch)
		}
	}

	return result, nil
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
