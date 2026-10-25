package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// mergeInProgressFn is injected for testing. In production, it checks the real
// git state. In tests, it can be overridden to return false to avoid git state
// bleeding into test isolation.
var mergeInProgressFn = isMergeInProgress

// featureStartPattern matches wipnote feature/bug start commands.
var featureStartPattern = regexp.MustCompile(`\bwipnote\s+(feature|bug)\s+start\s+([\w-]+)`)

// checkYoloStepsGuard warns when starting a work item that has no
// implementation steps. Returns a non-empty reason to warn, or "" to allow.
func checkYoloStepsGuard(event *CloudEvent, yolo bool, wipnoteDir string) string {
	if !yolo || !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	m := featureStartPattern.FindStringSubmatch(cmd)
	if m == nil {
		return ""
	}
	itemID := m[2]
	stepsCount := countStepsForItem(wipnoteDir, itemID)
	if stepsCount > 0 {
		return ""
	}
	return fmt.Sprintf(
		"Warning: %s has no implementation steps. "+
			"Add steps first: wipnote feature add-step %s \"description\"",
		itemID, itemID)
}

// countStepsForItem reads an HTML work item file and counts its steps.
func countStepsForItem(wipnoteDir, itemID string) int {
	subdirs := []string{"features", "bugs", "spikes", "tracks", "plans", "specs"}
	for _, sub := range subdirs {
		path := filepath.Join(wipnoteDir, sub, itemID+".html")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		return strings.Count(string(data), "data-step-id=")
	}
	return 0
}

// gitCommitPattern matches "git commit" as a standalone command.
// It is anchored to the start of the string (modulo leading whitespace) and
// requires that after "commit" comes whitespace, end-of-string, or a flag
// prefix (-- or -X where X is not a lowercase letter). This excludes git
// plumbing sub-commands like "git commit-tree" and "git commit-graph" where
// the dash is part of the sub-command name rather than a flag separator.
var gitCommitPattern = regexp.MustCompile(`^\s*git\s+commit(\s|$|--|-[^a-z])`)

// fallbackTestSuggestion is used when the project's language can't be
// detected from manifest files. It enumerates the supported test
// commands so the user can pick the relevant one rather than seeing
// a confidently-wrong single command (bug-f616c2a8).
const fallbackTestSuggestion = "your project's test suite (go test ./..., uv run pytest, npm test, cargo test, etc.)"

// checkYoloCommitGuard blocks git commit when tests haven't run in
// the current session. Returns a non-empty reason to block, or "" to allow.
//
// The error message names the test command for the detected project
// type (Go, Python, Node, Rust). Falls back to a generic enumeration
// when no manifest file is found in the project root or its monorepo
// subdirectories. Previously emitted "go test ./... or uv run pytest"
// regardless of project, which was confusing in single-language
// projects (bug-f616c2a8).
func checkYoloCommitGuard(event *CloudEvent, yolo, testRan bool) string {
	if !yolo {
		return ""
	}
	if !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if !gitCommitPattern.MatchString(cmd) {
		return ""
	}
	if testRan {
		return ""
	}
	suggestion := paths.TestCommandFor(paths.DetectProjectType(ResolveProjectDir(event.CWD, event.SessionID)))
	if suggestion == "" {
		suggestion = fallbackTestSuggestion
	}
	return "YOLO mode requires tests to pass before committing. Run: " + suggestion
}

// checkYoloBudgetGuard blocks git commit when the staged diff exceeds
// YOLO hard limits (20 files or 600 lines added). Merge commits are
// exempt — they combine already-reviewed sub-feature work.
func checkYoloBudgetGuard(event *CloudEvent, yolo bool) string {
	if !yolo || !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if !gitCommitPattern.MatchString(cmd) {
		return ""
	}
	if mergeInProgressFn() {
		return ""
	}
	out, err := exec.Command("git", "diff", "--cached", "--numstat").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var fileCount, totalAdded int
	for _, line := range lines {
		if line == "" {
			continue
		}
		fileCount++
		parts := strings.Fields(line)
		if len(parts) >= 1 && parts[0] != "-" {
			n, _ := strconv.Atoi(parts[0])
			totalAdded += n
		}
	}
	if fileCount > yoloBudgetMaxFiles || totalAdded > yoloBudgetMaxLines {
		return fmt.Sprintf(
			"YOLO budget HARD LIMIT: %d files, %d lines (max %d/%d). "+
				"Split into sub-features.", fileCount, totalAdded, yoloBudgetMaxFiles, yoloBudgetMaxLines)
	}
	return ""
}

// isMergeInProgress returns true when git is resolving a merge (MERGE_HEAD exists).
func isMergeInProgress() bool {
	out, err := exec.Command("git", "rev-parse", "--git-dir").Output()
	if err != nil {
		return false
	}
	gitDir := strings.TrimSpace(string(out))
	_, err = os.Stat(filepath.Join(gitDir, "MERGE_HEAD"))
	return err == nil
}

// checkYoloWorktreeGuard blocks Write/Edit on main/master branch in YOLO mode.
// Merge conflict resolution is exempt — edits on main during an active merge
// are integration work, not feature development.
func checkYoloWorktreeGuard(toolName, branch string, yolo bool) string {
	if !yolo {
		return ""
	}
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	if branch == "main" || branch == "master" {
		if mergeInProgressFn() {
			return ""
		}
		return "YOLO mode requires a feature or track branch. " +
			"Use: wipnote yolo --track <id> or wipnote yolo --feature <id>"
	}
	return ""
}

// checkYoloBashWorktreeGuard extends the worktree guard to Bash file-write
// commands on main/master branch.
// wipnote CLI commands are always exempt — they are the approved write path.
func checkYoloBashWorktreeGuard(event *CloudEvent, branch string, yolo bool) string {
	if !yolo {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if isWipnoteCLICommand(cmd) {
		return ""
	}
	if !isBashFileWrite(event) {
		return ""
	}
	if branch == "main" || branch == "master" {
		if mergeInProgressFn() {
			return ""
		}
		return "YOLO mode requires a feature or track branch for Bash file writes. " +
			"Use: wipnote yolo --track <id> or wipnote yolo --feature <id>"
	}
	return ""
}

// checkYoloOrchestratorWriteGuard warns (does not block) when the top-level
// orchestrator session writes files directly instead of delegating to a
// subagent. This is a soft enforcement of the "delegate, don't implement"
// rule — logged for observability but not blocking to avoid breaking
// non-YOLO or legitimate orchestrator writes.
func checkYoloOrchestratorWriteGuard(event *CloudEvent, isSubagent bool) string {
	if isSubagent {
		return "" // Subagents are expected to write files.
	}
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
		return "Orchestrator writing directly instead of delegating. " +
			"Consider using a coder agent for implementation work."
	}
	return ""
}

// checkYoloRoborevGuard blocks git commit when there are completed roborev
// reviews with findings (verdict == "F") from prior commits in this session.
// This is a "review gate, not a review wall": it only fires when roborev has
// already finished reviewing a prior commit and found issues. Entries with no
// verdict (still running) are not blocking. Any error (roborev not installed,
// daemon down, timeout) causes a fail-open return of "" to avoid blocking
// unrelated work.
func checkYoloRoborevGuard(event *CloudEvent, yolo bool) string {
	if !yolo || !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if !gitCommitPattern.MatchString(cmd) {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	roborevCmd := exec.CommandContext(ctx, "roborev", "list", "--open", "--json")
	if event.CWD != "" {
		roborevCmd.Dir = event.CWD
	}
	out, err := roborevCmd.Output()
	if err != nil {
		return "" // fail-open: not installed, daemon down, timeout, etc.
	}

	// RawID uses json.RawMessage so both string ("j1") and integer (46) IDs
	// from different roborev versions parse without type-mismatch errors.
	type roborevEntry struct {
		RawID         json.RawMessage `json:"id"`
		Verdict       string          `json:"verdict"`
		CommitSubject string          `json:"commit_subject"`
	}

	var entries []roborevEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return "" // fail-open: unexpected output format
	}

	var failedIDs []string
	for _, e := range entries {
		if e.Verdict == "F" {
			// Strip surrounding quotes for string IDs; numbers pass through as-is.
			failedIDs = append(failedIDs, strings.Trim(string(e.RawID), `"`))
		}
	}
	if len(failedIDs) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"roborev: %d open review(s) with findings — fix before committing (job IDs: %s). "+
			"Run /roborev-fix to address them.",
		len(failedIDs), strings.Join(failedIDs, ", "))
}

// checkYoloDiffReviewGuard blocks git commit when no git diff has been
// reviewed in this session.
func checkYoloDiffReviewGuard(event *CloudEvent, yolo, diffRan bool) string {
	if !yolo || !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if !gitCommitPattern.MatchString(cmd) {
		return ""
	}
	if diffRan {
		return ""
	}
	return "YOLO mode requires a diff review before committing. " +
		"Run: git diff --stat"
}

// checkYoloCodeHealthGuard warns about oversized source files (>yoloCodeHealthMaxLines)
// in YOLO mode. When a file is already oversized, the guard allows edits to
// proceed — blocking would prevent the refactoring needed to reduce file size.
// The warning message is returned for logging in pretooluse; PostToolUse
// performs the actual enforcement via CheckFileQuality.
// Covers Go, Python, JavaScript, and TypeScript files.
func checkYoloCodeHealthGuard(event *CloudEvent, yolo bool) string {
	if !yolo {
		return ""
	}
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	path, _ := event.ToolInput["file_path"].(string)
	if path == "" {
		path, _ = event.ToolInput["path"].(string)
	}
	if !isCodeHealthCheckedFile(path) {
		return ""
	}
	// Check existing file size — if it's already >yoloCodeHealthMaxLines, warn
	// but allow (blocking would prevent the refactoring needed to fix it).
	data, err := os.ReadFile(path)
	if err != nil {
		return "" // new file, allow
	}
	lines := strings.Count(string(data), "\n")
	if lines > yoloCodeHealthMaxLines {
		return fmt.Sprintf(
			"Code health: %s has %d lines (limit %d). Consider refactoring into smaller modules.",
			filepath.Base(path), lines, yoloCodeHealthMaxLines)
	}
	return ""
}

// isCodeHealthCheckedFile returns true for file extensions that are subject
// to the YOLO code-health line-count guard.
func isCodeHealthCheckedFile(path string) bool {
	for _, ext := range []string{".go", ".py", ".js", ".ts", ".tsx", ".jsx"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// genericAgentIDs lists harness-level agent identifiers that must not be used
// as a cross-session bridge — they appear in many unrelated sessions.
var genericAgentIDs = []string{"claude-code", "claude", "codex", "gemini", "antigravity", "human"}

// isGenericAgentID returns true when id is one of the well-known harness
// identifiers that should never be used for cross-session matching.
func isGenericAgentID(id string) bool {
	return slices.Contains(genericAgentIDs, id)
}
