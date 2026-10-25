package hooks

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/paths"
)

// checkProjectDivergence compares the CWD of the current event against the
// project_dir stored in the session row. When they resolve to different
// .wipnote/ roots:
//   - Write tools are blocked with a clear error message.
//   - Read-only tools are silently allowed, but a warning is written to debug.log.
//
// Returns nil to allow the event to proceed.
func checkProjectDivergence(event *CloudEvent, database *sql.DB, sessionID string) *HookResult {
	if sessionID == "" || event.CWD == "" {
		return nil
	}

	sess, err := db.GetSession(database, sessionID)
	if err != nil || sess == nil || sess.ProjectDir == "" {
		// No stored project_dir — nothing to compare against.
		return nil
	}

	eventProjectDir := ResolveProjectDir(event.CWD, event.SessionID)
	sessionProjectDir := sess.ProjectDir

	// Strip the "unresolved:" sentinel that NormalizeProjectDir adds when the
	// project root is a host-path with no anchor (typical in test tempdirs).
	// ResolveProjectDir returns raw paths; comparing must happen on the same
	// shape.
	sessionProjectDir = strings.TrimPrefix(sessionProjectDir, "unresolved:")
	eventProjectDir = strings.TrimPrefix(eventProjectDir, "unresolved:")

	if !projectsDiverge(event.CWD, eventProjectDir, sessionProjectDir) {
		return nil
	}

	if isWriteTool(event.ToolName) {
		return &HookResult{
			Decision: "block",
			Reason: fmt.Sprintf(
				"CWD has changed to a different project (%s). "+
					"Start a new session in that project.",
				eventProjectDir,
			),
		}
	}

	// Read-only tool: allow but log the drift.
	debugLog(sessionProjectDir, "[wipnote] CWD divergence (read-only %s): session=%s event_cwd=%s",
		event.ToolName, sessionProjectDir, event.CWD)
	return nil
}

// projectsDiverge reports whether the event's project dir is a genuinely
// different wipnote project from the session's.
func projectsDiverge(eventCWD, eventProjectDir, sessionProjectDir string) bool {
	if eventProjectDir == sessionProjectDir {
		return false
	}

	// Normalise both paths to eliminate symlink / trailing-slash differences.
	cleanEvent := filepath.Clean(eventProjectDir)
	cleanSession := filepath.Clean(sessionProjectDir)
	if cleanEvent == cleanSession {
		return false
	}

	// Worktree-aware: linked worktrees and their main repo share a .git
	// common dir and therefore the same logical project. Normalising both
	// paths through ResolveViaGitCommonDir collapses worktree↔main and
	// worktreeA↔worktreeB to the same canonical root. Without this check,
	// a YOLO session that records sess.ProjectDir=<main> in SessionStart
	// then fires events with WIPNOTE_PROJECT_DIR=<worktree> sees a false
	// divergence and blocks every Write/Edit (bug-a1993e6b — wedged
	// YOLO session on feat-5ddde9d7 / 2026-05-13).
	if canonicalRepoRoot(cleanEvent) == canonicalRepoRoot(cleanSession) {
		return false
	}

	// Scratch-space bypass: if the raw event CWD has no .wipnote/ ancestor,
	// the user has moved to an untracked directory (e.g. /tmp). That is
	// legitimate scratch-space usage — do not block. We check event.CWD
	// directly (before ResolveProjectDir's walk-up logic) so that a scratch
	// path like /tmp/notes is not mistaken for the current process's project.
	// The guard only applies when both the session and the CWD are independently
	// wipnote-tracked projects.
	return hasWipnoteAnchor(filepath.Clean(eventCWD))
}

// canonicalRepoRoot returns the main repository root for any path that lives
// inside the same logical project (whether the path is the main checkout or
// a linked worktree of it). Used by checkProjectDivergence so that worktrees
// don't trigger a false-positive "different project" block.
//
// Behaviour:
//   - Relative paths are first resolved to absolute via filepath.Abs so that
//     "." doesn't depend on the hook process's CWD at invocation time.
//   - For a linked worktree, paths.ResolveViaGitCommonDir returns the main
//     repo root (parent of the shared .git dir).
//   - For the main checkout (or any non-worktree path that already has its
//     own .git), ResolveViaGitCommonDir returns "" — in that case we treat
//     the absolute path itself as the canonical root after symlink resolution.
//
// Two paths in the same logical project produce the same canonical root via
// this helper; paths in unrelated projects produce different roots.
func canonicalRepoRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = filepath.Clean(dir)
	}
	if eval, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		abs = eval
	}
	if main := paths.ResolveViaGitCommonDir(abs); main != "" {
		if eval, evalErr := filepath.EvalSymlinks(main); evalErr == nil {
			return eval
		}
		return main
	}
	return abs
}

// hasWipnoteAnchor reports whether dir or any of its parent directories
// contains a .wipnote/ subdirectory. Used by checkProjectDivergence to
// distinguish a "different wipnote project" (should block) from an untracked
// scratch directory like /tmp (should allow).
func hasWipnoteAnchor(dir string) bool {
	d := dir
	for {
		if _, err := os.Stat(filepath.Join(d, ".wipnote")); err == nil {
			return true
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return false
}
