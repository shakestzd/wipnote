package hooks

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
)

// checkYoloResearchGuard blocks Write/Edit when no Read/Grep/Glob has
// occurred in the session (research-first principle). PreToolUse blocks on
// it only in YOLO mode and adds it as advice otherwise.
//
// targetFile is the Write/Edit target path extracted from the tool input.
// When the target is outside the project root (home config, /tmp, sibling repos,
// etc.) the guard is skipped — research-first discipline applies to project code,
// not to external config or memory files.
// projectRoot is the resolved project directory (ctx.ProjectDir); when empty
// the guard applies unconditionally (conservative).
func checkYoloResearchGuard(toolName string, _ bool, hasResearch bool, targetFile, projectRoot string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	// Skip for writes targeting paths outside the project root.
	if targetFile != "" && pathIsOutsideProject(targetFile, projectRoot) {
		return ""
	}
	if hasResearch {
		return ""
	}
	return "Research is required before writing code. " +
		"Read the relevant code (Read/Grep/Glob) and/or consult official docs, " +
		"GitHub issues, or the web (WebSearch/WebFetch, `gh search`) — especially " +
		"for external libraries, upstream tools, or unfamiliar error messages. " +
		"Bash read commands also count: `ls`, `find`, `cat`, `grep`, `head`, `tail`, " +
		"`stat`, `sed` (read-only, not `sed -i`), `gh`, `curl`, `wipnote sh`, and " +
		"`wipnote search`."
}

// isExternalTechEdit reports whether a Write/Edit target is a dependency
// manifest — the durable, low-false-positive signal that the edit introduces or
// changes an external technology dependency. Following the project's "prefer
// durable file/diff state over brittle session/substring state" hook rule,
// detection is keyed on the target file's basename rather than fuzzy
// library-name matching against prompt text. The canonical manifest set lives in
// paths.IsDependencyManifest, shared with the pre-commit and completion gates.
func isExternalTechEdit(targetFile string) bool {
	return paths.IsDependencyManifest(targetFile)
}

// applyPatchFileHeaderRe matches the file headers of a Codex apply_patch envelope
// (`*** Add File: <path>`, `*** Update File: <path>`, `*** Delete File: <path>`).
var applyPatchFileHeaderRe = regexp.MustCompile(`(?m)^\*\*\*\s+(?:Add|Update|Delete)\s+File:\s*(.+?)\s*$`)

// applyPatchMoveRe matches the rename target of an apply_patch update
// (`*** Move to: <path>`).
var applyPatchMoveRe = regexp.MustCompile(`(?m)^\*\*\*\s+Move\s+to:\s*(.+?)\s*$`)

// applyPatchTouchedPaths extracts every file path referenced by a Codex
// apply_patch payload. Codex bundles multiple file edits in one call, so the
// path lives in the patch body rather than a file_path field — without this the
// research guards would see an empty target and silently skip apply_patch
// manifest/harness edits (roborev #563/#566).
func applyPatchTouchedPaths(patch string) []string {
	if patch == "" {
		return nil
	}
	var paths []string
	for _, m := range applyPatchFileHeaderRe.FindAllStringSubmatch(patch, -1) {
		if p := strings.TrimSpace(m[1]); p != "" {
			paths = append(paths, p)
		}
	}
	for _, m := range applyPatchMoveRe.FindAllStringSubmatch(patch, -1) {
		if p := strings.TrimSpace(m[1]); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// editTargetPaths returns every file path a Write/Edit-class tool will modify.
// For apply_patch it parses the patch payload (paths are not in a file_path
// field); for Write/Edit/MultiEdit it returns the single extractFilePath result.
// Returns nil for non-edit tools or when no path can be resolved.
func editTargetPaths(event *CloudEvent) []string {
	if event == nil {
		return nil
	}
	if event.ToolName == "apply_patch" {
		patch, _ := event.ToolInput["patch"].(string)
		return applyPatchTouchedPaths(patch)
	}
	if p := extractFilePath(event.ToolInput); p != "" {
		return []string{p}
	}
	return nil
}

// firstPathMatching returns the first path satisfying pred, or "" if none do.
func firstPathMatching(paths []string, pred func(string) bool) string {
	for _, p := range paths {
		if pred(p) {
			return p
		}
	}
	return ""
}

// checkExternalTechResearchGuard requires at least one web/docs research call
// (WebSearch/WebFetch or `gh ...`) before a Write/Edit that modifies a
// dependency manifest. A plain local Read does NOT satisfy this gate: the
// always-on research guard (checkYoloResearchGuard) accepts any read, but
// external-technology changes demand current upstream docs/changelogs that a
// local read cannot provide (spk-0a982f70 root cause: one `cat go.mod` cleared
// the only always-on research gate, so web research was never actually
// required).
//
// Non-dependency-manifest edits keep the existing any-read behavior (this guard
// returns "" for them, so there is no regression). Writes outside the project
// root are skipped, mirroring checkYoloResearchGuard. PreToolUse blocks on it
// only in YOLO mode and adds it as advice otherwise.
func checkExternalTechResearchGuard(toolName string, hasWebResearch bool, targetFile, projectRoot string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	if targetFile != "" && pathIsOutsideProject(targetFile, projectRoot) {
		return ""
	}
	if !isExternalTechEdit(targetFile) {
		return ""
	}
	if hasWebResearch {
		return ""
	}
	return "Research is required before changing dependencies. Editing " +
		filepath.Base(targetFile) + " adds or changes an external technology, so " +
		"verify current official docs/changelogs via the web (WebSearch/WebFetch, " +
		"`gh search`) this session before writing — a local file read does NOT " +
		"satisfy this gate."
}

// harnessContractPathSignals are path substrings that mark a file as encoding an
// AI-harness integration contract (Claude Code / Codex / Gemini). These schemas
// drift silently across vendor releases — a field set in an agent manifest or
// hook config may stop being honored with no error — so edits to them MUST be
// preceded by web research against current provider docs (see the "Monitoring
// Upstream Harnesses" mandate in CLAUDE.md). The list is intentionally narrow to
// keep false-positives low: only genuine harness-contract source files match.
var harnessContractPathSignals = []string{
	"plugin/agents/",            // agent frontmatter — per-harness schema contract
	"plugin/hooks/",             // hook event wiring
	"plugin-core/manifest.json", // hook event matrix + per-target output paths
	"pluginbuild/",              // per-harness plugin/agent generation logic
	"/prompts/system-prompt",    // harness system prompt
}

// isHarnessContractEdit reports whether a Write/Edit target is a harness-contract
// source file. Like isExternalTechEdit it keys on the durable target path rather
// than fuzzy prompt/content substring matching, per the project's hook-state
// rule (prefer file/diff state over brittle session/substring state).
func isHarnessContractEdit(targetFile string) bool {
	if targetFile == "" {
		return false
	}
	p := filepath.ToSlash(targetFile)
	for _, sig := range harnessContractPathSignals {
		if strings.Contains(p, sig) {
			return true
		}
	}
	return false
}

// checkHarnessContractResearchGuard flags a Write/Edit to a harness-contract
// file when no web/docs research has happened this session — the inverse of the
// non-blocking checkOrchestratorResearchDelegationAdvisory. Harness contracts
// (agent manifests, hook matrices, the plugin generator, system prompts) drift
// silently across vendor releases, so contract-touching work is escalated from
// an advisory nudge to a hard block until current provider docs are consulted
// (feat-ff62b911 / spk-0a982f70).
//
// Non-harness-contract edits return "" (no regression). Writes outside the
// project root are skipped, mirroring the other research guards. PreToolUse
// blocks on it only in YOLO mode and adds it as advice otherwise.
// False-positives are kept low by matching only the narrow
// harnessContractPathSignals set.
func checkHarnessContractResearchGuard(toolName string, hasWebResearch bool, targetFile, projectRoot string) string {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	if targetFile != "" && pathIsOutsideProject(targetFile, projectRoot) {
		return ""
	}
	if !isHarnessContractEdit(targetFile) {
		return ""
	}
	if hasWebResearch {
		return ""
	}
	return "Web research is required before harness-contract work. Editing " +
		filepath.Base(targetFile) + " changes a Claude Code / Codex / Gemini " +
		"integration contract, which drifts silently across vendor releases — " +
		"verify the current provider docs with WebSearch/WebFetch (or `gh search`) " +
		"this session before writing. A local file read does NOT satisfy this gate."
}

// checkYoloBashResearchGuard extends the research guard to Bash file-write commands.
// Blocks in YOLO mode, advises otherwise. wipnote CLI commands and osascript
// invocations are exempt.
//
// Writes whose first path target lies outside the project tree (e.g. /tmp/…,
// ~/.config/…) are skipped, mirroring the pathIsOutsideProject skip on the
// Write/Edit research guard: the code-research gate protects project source,
// and Read/Grep/Glob cannot reach external paths anyway (bug-d0c8b1e2, GH-#97).
func checkYoloBashResearchGuard(event *CloudEvent, _ bool, hasResearch bool) string {
	cmd := shellCommand(event.ToolInput)
	if isWipnoteCLICommand(cmd) || isOsascriptCommand(cmd) {
		return ""
	}
	if !isBashFileWrite(event) {
		return ""
	}
	if hasResearch {
		return ""
	}
	projectRoot := ResolveProjectDir(event.CWD, event.SessionID)
	if bashCommandTargetsExternalPath(cmd, projectRoot) {
		return ""
	}
	return "Research is required before writing code via Bash. " +
		"Read the relevant code (Read/Grep/Glob) and/or consult official docs, " +
		"GitHub issues, or the web (WebSearch/WebFetch, `gh search`) — especially " +
		"for external libraries, upstream tools, or unfamiliar error messages."
}

// pathIsOutsideProject returns true when path refers to a location outside the
// project root. It is the shared classification heuristic used by both the
// Bash research guard and the Write/Edit work-item and research guards.
//
// Rules (applied in order):
//  1. Empty path → false (cannot classify, treat as in-project).
//  2. ~/- or ~\-prefixed home paths are external, EXCEPT allow-listed dirs
//     (~/.gotest, ~/.tmp, ~/.cache) which are test/temp scratch space.
//  3. Absolute paths inside the project root (via filepath.Rel) → internal.
//  4. Absolute paths in /workspaces/ siblings when the project is also under
//     /workspaces/ → internal (Codespaces convention).
//  5. All other absolute paths → external.
//  6. Relative paths → false (treated as in-project; caller is responsible for
//     resolving against CWD if needed).
func pathIsOutsideProject(path, projectRoot string) bool {
	if path == "" {
		return false
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		// Allow common test/temp directories in home.
		if strings.HasPrefix(path, "~/.gotest") || strings.HasPrefix(path, "~/.tmp") || strings.HasPrefix(path, "~/.cache") {
			return false
		}
		return true
	}
	if strings.HasPrefix(path, "/") {
		// Allow siblings in /workspaces/ if the project itself is in /workspaces/.
		if strings.HasPrefix(projectRoot, "/workspaces/") && strings.HasPrefix(path, "/workspaces/") {
			return false
		}
		// Resolve against project root: if the path is inside the project,
		// it is internal — not an external write.
		if projectRoot != "" {
			rel, err := filepath.Rel(projectRoot, path)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return false // in-repo absolute path
			}
		}
		return true
	}
	// Relative path — treat as in-project.
	return false
}

// bashCommandTargetsExternalPath returns true when the Bash command's first
// path-like argument starts with a home-directory shorthand (~) or is an absolute
// path that falls outside the project root. It is a best-effort heuristic that
// stops at the FIRST path-like token and must err on the side of false
// negatives (a false negative only means the research guard still applies).
//
// projectRoot is used to classify absolute paths: paths inside the project root
// are considered internal (returns false); paths outside are external (returns true).
// When projectRoot is empty, any absolute path is treated as external.
func bashCommandTargetsExternalPath(cmd, projectRoot string) bool {
	// Look for the first argument that looks like a path (starts with ~ or /).
	for _, field := range strings.Fields(cmd) {
		if pathIsOutsideProject(field, projectRoot) {
			return true
		}
		// pathIsOutsideProject returns false for relative paths AND for in-project
		// absolute paths. For the Bash heuristic we must stop at the first
		// path-like token (~ or /), so only continue scanning for non-path tokens.
		if strings.HasPrefix(field, "~/") || strings.HasPrefix(field, "~\\") || strings.HasPrefix(field, "/") {
			return false
		}
	}
	return false
}
