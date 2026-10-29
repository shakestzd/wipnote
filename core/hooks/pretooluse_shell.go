package hooks

import (
	"regexp"
	"strings"
)

// checkBashCwdGuard detects Bash commands that would permanently change the
// working directory. Bare `cd dir && cmd` pollutes CWD for all subsequent
// tool calls in the session. Subshells `(cd dir && cmd)` are safe.
//
// Returns a non-empty reason string to block the command, or "" to allow.
func checkBashCwdGuard(event *CloudEvent) string {
	if !isShellTool(event.ToolName) {
		return ""
	}
	cmd := shellCommand(event.ToolInput)
	if cmd == "" {
		return ""
	}
	if !bareCdPattern.MatchString(cmd) {
		return ""
	}
	return "Bare `cd` changes the working directory permanently. " +
		"Use a subshell instead: `(cd dir && command)` — " +
		"this returns to the original directory when done."
}

// bareCdPattern matches a bare `cd` at the start of a command that is NOT
// wrapped in a subshell. It does NOT match:
//   - (cd dir && cmd)   — subshell, safe
//   - cd /absolute/path && pwd  — going to project root is fine... actually still bad
//
// It matches:
//   - cd some-dir && go build
//   - cd dir && cmd1 && cmd2
var bareCdPattern = regexp.MustCompile(`^cd\s+[^;)]+&&`)

// iswipnoteWrite returns true for file-write tools targeting .wipnote/.
func iswipnoteWrite(event *CloudEvent) bool {
	switch event.ToolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return false
	}
	if event.ToolName == "apply_patch" {
		patch, _ := event.ToolInput["patch"].(string)
		return containsWipnoteDir(patch)
	}
	path, _ := event.ToolInput["path"].(string)
	if path == "" {
		path, _ = event.ToolInput["file_path"].(string)
	}
	return containsWipnoteDir(path)
}

func containsWipnoteDir(path string) bool {
	for i := range path {
		if path[i] == '.' && i+9 <= len(path) && path[i:i+9] == ".wipnote/" {
			return true
		}
	}
	return path == ".wipnote"
}

// isBashwipnoteWrite detects Bash commands that mutate .wipnote/ files
// directly (rm, sed -i, redirects, mv, cp, git add/rm/mv/restore/checkout/
// stash, python -c, …). These bypass the wipnote CLI and the commit-queue
// outbox and must be blocked. The decision is made per shell segment on the
// resolved operation targets — see store_guard.go (GH-#180).
func isBashwipnoteWrite(event *CloudEvent) bool {
	if !isShellTool(event.ToolName) {
		return false
	}
	cmd := shellCommand(event.ToolInput)
	if cmd == "" {
		return false
	}
	return bashCommandWritesWipnoteStore(cmd)
}

// isWipnoteCLICommand returns true when every shell command segment invokes the
// wipnote CLI binary. The check is intentionally anchored to the executable
// token so commands that merely mention "wipnote" cannot bypass write guards.
func isWipnoteCLICommand(cmd string) bool {
	segments := splitShellCommandSegments(cmd)
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if !segmentStartsWithWipnoteCLI(segment) {
			return false
		}
	}
	return true
}

// splitShellCommandSegments splits cmd on unquoted ;, |, &&, and newlines.
// A single- or double-quoted span is never split, even if it contains a
// separator character — "Never; rm .wipnote/x" is one word, not a command
// boundary, matching how a real shell would parse it. Only the SAME quote
// character that opened a span closes it; nested different-quote-inside-
// same-quote and backslash-escaped quotes are not modeled, matching the
// simplifications tokenizeShellWords already makes for word-splitting.
func splitShellCommandSegments(cmd string) []string {
	var segments []string
	start := 0
	var quote byte
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		sepLen := 0
		switch c {
		case '\n', ';', '|':
			sepLen = 1
		case '&':
			if i+1 < len(cmd) && cmd[i+1] == '&' {
				sepLen = 2
			}
		}
		if sepLen == 0 {
			continue
		}
		part := strings.TrimSpace(cmd[start:i])
		if part != "" {
			segments = append(segments, part)
		}
		i += sepLen - 1
		start = i + 1
	}
	part := strings.TrimSpace(cmd[start:])
	if part != "" {
		segments = append(segments, part)
	}
	return segments
}

func segmentStartsWithWipnoteCLI(segment string) bool {
	fields := strings.Fields(segment)
	for len(fields) > 0 {
		first := fields[0]
		if strings.Contains(first, "=") && !strings.HasPrefix(first, "-") {
			fields = fields[1:]
			continue
		}
		return first == "wipnote" || strings.HasSuffix(first, "/wipnote")
	}
	return false
}

// isBashFileWrite detects Bash commands that modify source files (as opposed
// to read-only commands like git status, ls, grep, etc.). Used by YOLO guards
// to extend Write/Edit/MultiEdit protections to Bash file manipulation.
func isBashFileWrite(event *CloudEvent) bool {
	if !isShellTool(event.ToolName) {
		return false
	}
	cmd := shellCommand(event.ToolInput)
	if cmd == "" {
		return false
	}
	// osascript drives macOS apps (Notes, Mail, …) over Apple events; its
	// AppleScript/HTML payloads are not filesystem writes (GH-#97).
	if isOsascriptCommand(cmd) {
		return false
	}
	return bashFileWritePattern.MatchString(cmd)
}

// isOsascriptCommand reports whether every segment of cmd (after joining
// backslash continuations and dropping heredoc bodies) invokes osascript. A
// compound command that chains osascript with anything else is NOT exempt.
func isOsascriptCommand(cmd string) bool {
	joined := strings.ReplaceAll(cmd, "\\\n", " ")
	segments := splitShellCommandSegments(stripHeredocBodies(joined))
	if len(segments) == 0 {
		return false
	}
	for _, segment := range segments {
		if name, _ := splitShellCommandArgs(tokenizeShellWords(segment)); name != "osascript" {
			return false
		}
	}
	return true
}

// isShellTool reports whether toolName is the harness-native shell invocation
// tool. Each harness uses a different name:
//   - Claude Code: "Bash"
//   - Codex: "exec_command" / "functions.exec_command"
//   - Gemini: "run_shell_command"
//   - Antigravity: "run_command" (Antigravity translates run_shell_command →
//     run_command in its generated agent manifests)
//
// This predicate gates shell-only guards (dependency-research check, YOLO commit
// check, shell file-write protection). Keep in sync with researchShellToolNamesSQL
// in yolo_guard.go (issue #144).
func isShellTool(toolName string) bool {
	switch toolName {
	case "Bash", "exec_command", "functions.exec_command",
		"run_shell_command", "run_command":
		return true
	}
	return false
}

func shellCommand(input map[string]any) string {
	if input == nil {
		return ""
	}
	for _, key := range []string{"command", "cmd"} {
		if v, ok := input[key].(string); ok {
			return v
		}
	}
	return ""
}

// bashFileWritePattern matches Bash commands that write/modify files.
// Uses a write-intent denylist: known destructive commands are matched; pure
// inspection commands (ls, cat, head, tail, stat, find, grep, etc.) are not.
//
// Redirect detection:
//   - `(?:^|\s|;|&&|\|\|)>>?\s*[^&\s]` matches plain shell output redirects
//     (> and >>) preceded by a word boundary. Handles both `cmd > file` and `cmd >file`.
//   - `(?:^|\s|;|&&|\|\|)1>>?\s*[^&\s]` matches explicit fd-1 (stdout) redirects:
//     `1>file`, `1>>file`. It is anchored to a word start so `<h1>` in a payload
//     does not match. We target fd 1 specifically to avoid false-positives on
//     benign `2>/dev/null` patterns (the existing exclusion for `2>/dev/null`-shape
//     stderr redirects is preserved since we don't add a generic `[0-9]+>` pattern).
//   - `&>>?\s*[^\s]` matches `&>file` and `&>>file` (stdout+stderr combined redirect).
//     Excludes fd-to-fd `>&N` because the `&` must immediately precede `>`.
//   - fd-to-fd redirects like `>&2` are excluded because the existing pattern requires
//     a non-`&`, non-whitespace character after the redirect operator.
//   - `find ... -delete` is a destructive option that removes matching files.
var bashFileWritePattern = regexp.MustCompile(
	`(?:` +
		// In-place editors
		`\bsed\s+-i` +
		`|` +
		`\bperl\s+-[pi]` +
		`|` +
		`\bawk\s+-i` +
		`|` +
		// Shell output redirects (both > and >>), handling spaces around >
		`(?:^|\s|;|&&|\|\|)>>?\s*[^&\s]` +
		`|` +
		// Explicit fd-1 (stdout) redirects: 1>file, 1>>file. Anchored to the
		// start of a word so the "1>" inside an HTML/AppleScript payload such
		// as "<h1>Title</h1>" does not match (GH-#97), and excluding "&" so the
		// fd-dup form 1>&2 is not treated as a file write. fd 1 specifically
		// avoids matching benign 2>/dev/null patterns.
		`(?:^|\s|;|&&|\|\|)1>>?\s*[^&\s]` +
		`|` +
		// Combined stdout+stderr redirects: &>file and &>>file
		// Excludes >&N (fd-to-fd) because that form has > after &, not & before >.
		`&>>?\s*[^\s]` +
		`|` +
		// File removal / relocation / creation
		`\brm\s` +
		`|` +
		`\bcp\s` +
		`|` +
		`\bmv\s` +
		`|` +
		`\bln\s` +
		`|` +
		`\binstall\s` +
		`|` +
		// Permission / ownership changes
		`\bchmod\s` +
		`|` +
		`\bchown\s` +
		`|` +
		// Pipe-to-file writers
		`\btee\s` +
		`|` +
		`\bdd\s` +
		`|` +
		`\bpatch\s` +
		`|` +
		// Common formatter/linter commands that rewrite files in place.
		`\bgofmt\s+-w\b` +
		`|` +
		`\bgo\s+fmt\b` +
		`|` +
		`\bprettier\b.*\s--write\b` +
		`|` +
		`\beslint\b.*\s--fix\b` +
		`|` +
		`\bruff\b.*\s--fix\b` +
		`|` +
		`\bblack\b\s` +
		`|` +
		// Git write operations (add/commit/push modify index, history, or remote)
		`\bgit\s+(?:add|commit|push|reset|rebase|merge|mv|rm|stash|tag|cherry-pick)\b` +
		`|` +
		// Python one-liners that open files for writing
		`\bpython[23]?\s+-c\s+.*(?:open|write)` +
		`|` +
		// find -delete removes matching files
		`\bfind\b.*\s-delete\b` +
		`)`,
)
