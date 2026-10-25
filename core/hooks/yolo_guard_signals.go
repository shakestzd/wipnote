package hooks

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// hasRecentDiffReview reports whether a diff or show command was run in this
// session or its family root, per the canonical tool-events log. The hook DB is
// an empty tables-only projection, so agent_events cannot answer this
// (bug-a3b17225). No recorded evidence counts as "no review".
func hasRecentDiffReview(projectDir, sessionID string) bool {
	return canonicalShellCommandSeen(projectDir, sessionID, "git diff", "git show")
}

// canonicalShellCommandSeen reports whether any in-scope recorded shell call
// contains one of the needles.
func canonicalShellCommandSeen(projectDir, sessionID string, needles ...string) bool {
	seen, _ := canonicalAnySeen(projectDir, sessionID, func(ev canonicalToolEvent) bool {
		if !isShellTool(ev.Tool) {
			return false
		}
		cmd := ev.Summary + " " + ev.Input
		for _, n := range needles {
			if strings.Contains(cmd, n) {
				return true
			}
		}
		return false
	})
	return seen
}

// currentBranchIn returns the git branch for the given directory.
func currentBranchIn(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// branchForFilePath returns the git branch for the worktree that owns filePath.
// When the file lives in a linked worktree (e.g. .claude/worktrees/yolo-feat-xxx),
// this returns that worktree's branch rather than the main repo's branch.
// Falls back to cwdBranch when filePath is empty or not under git control.
func branchForFilePath(filePath, cwdBranch string) string {
	if filePath == "" {
		return cwdBranch
	}
	dir := filepath.Dir(filePath)
	branch := currentBranchIn(dir)
	if branch == "" {
		return cwdBranch
	}
	return branch
}

// hasRecentTestRun reports whether a test/build command was run in this
// session or its family root, per the canonical tool-events log (the hook DB
// is an empty projection; see hasRecentDiffReview). No evidence counts as
// "no test run".
func hasRecentTestRun(projectDir, sessionID string) bool {
	return canonicalShellCommandSeen(projectDir, sessionID,
		"go test", "go build", "pytest", "uv run ruff")
}

// uiFileExtensions are the file extensions considered UI files for the purpose
// of visual validation gating. A commit that only stages backend files skips
// the screenshot requirement entirely.
var uiFileExtensions = []string{".html", ".css", ".tsx", ".jsx", ".vue", ".svelte", ".js", ".ts"}

// hasStagedUIFiles runs git diff --cached --name-only and checks whether any
// staged file has a UI extension or lives under a UI directory (templates/,
// dashboard/). Returns false on any error so the gate degrades to allow.
func hasStagedUIFiles() bool {
	out, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		// Skip data files — .wipnote/ HTML files are work items, not UI.
		if strings.Contains(lower, ".wipnote/") {
			continue
		}
		// UI directories.
		if strings.Contains(lower, "templates/") || strings.Contains(lower, "dashboard/") {
			return true
		}
		// UI file extensions.
		for _, ext := range uiFileExtensions {
			if strings.HasSuffix(lower, ext) {
				return true
			}
		}
	}
	return false
}

// checkYoloUIValidationGuard blocks git commit when UI files are staged and no
// screenshot or visual validation was performed in the session.
// Returns a non-empty reason to block, or "" to allow.
//
// Fix: three structural problems were repaired (bug-a10ae96a / GH#36):
//  1. Staged-diff precheck — fires only when UI files are actually staged;
//     backend-only commits pass immediately without screenshot requirement.
//  2. Bash scope — gitCommitPattern is now anchored to `^\s*git\s+commit`
//     with a suffix guard that rejects "commit-tree" and "commit-graph"
//     (plumbing sub-commands). "gh issue create" and similar never matched.
//  3. Screenshot detection — the old LIKE '%screenshot%' pattern never matched
//     the only available Chrome MCP tool (mcp__claude-in-chrome__computer).
//     Now also checks tool_name = 'mcp__claude-in-chrome__computer' with
//     "action":"screenshot" in the tool_input JSON column. Existing
//     take_screenshot patterns are retained for other MCP server flavours.
func checkYoloUIValidationGuard(event *CloudEvent, yolo bool, sessionID, projectDir string) string {
	if !yolo || !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if !gitCommitPattern.MatchString(cmd) {
		return ""
	}

	// Fix 1: precheck the actual staged diff before touching session state.
	// If no UI files are staged, the gate is a no-op — backend-only commits
	// pass immediately without needing a screenshot.
	if !hasStagedUIFiles() {
		return ""
	}

	if !sessionTouchedUIFiles(sessionID, projectDir) {
		return "" // no UI files touched in this session (or cannot verify)
	}
	if sessionHasVisualValidation(sessionID, projectDir) {
		return ""
	}

	return "UI files were modified but no visual validation was performed. " +
		"Take a screenshot before committing."
}

// sessionTouchedUIFiles reports whether this session edited a UI file, per the
// canonical tool log (bug-a3b17225). .wipnote/ HTML is work-item data, not UI,
// and is excluded. With no canonical log this returns false and the gate stays
// open.
//
// It used to count agent_events rows first, but the only caller (pretooluse)
// has no hydrated index — no handle at all since feat-af8d1689 — so that count
// was always zero.
func sessionTouchedUIFiles(sessionID, projectDir string) bool {
	seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalUIEditEvent)
	return ok && seen
}

// sessionHasVisualValidation reports whether a screenshot was taken in this
// session or its family, per the canonical tool log. Supported patterns:
//   - input contains "action":"screenshot" (Chrome MCP, including browser_batch)
//   - tool name contains "screenshot" (other MCP flavours)
//
// It fails OPEN (true) when there is no canonical log, so a recording gap
// never blocks a commit.
func sessionHasVisualValidation(sessionID, projectDir string) bool {
	seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalScreenshotEvent)
	if !ok {
		return true // recording gap — cannot verify, do not block
	}
	return seen
}
