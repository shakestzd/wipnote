// Package testenv makes test binaries hermetic against the live wipnote /
// harness session that runs them.
//
// This project dogfoods itself: `go test` is routinely run from inside a
// Claude Code / Codex session launched by `wipnote claude`, and inherits that
// session's environment. A leaked variable silently changes what the code
// under test resolves — a WIPNOTE_AGENT_ID marks every work-item start as a
// subagent start (issue #194), and a CLAUDE_PROJECT_DIR points hook tests at
// the developer's real .wipnote/ store, where earlier runs left canonical tool
// logs that flip the research gate (issue #195). CI runners have none of these
// variables, so the suite passes there and fails only in a live session.
//
// ClearAmbientSession unsets them all once, from TestMain, so a test sees the
// same environment locally and in CI. Tests that exercise one of these
// variables set it explicitly with t.Setenv, which layers on top.
package testenv

import (
	"os"
	"strings"
)

// clearedPrefixes are variable-name prefixes wipnote owns outright. Every
// variable under them is session runtime state, never test configuration, so
// clearing by prefix also covers variables added after this list was written.
var clearedPrefixes = []string{"WIPNOTE_"}

// ClearedNames are the harness-owned variables wipnote reads to resolve the
// project, the session and the harness. They are listed by name because the
// harnesses' prefixes (CLAUDE_, CODEX_) also cover unrelated configuration.
var ClearedNames = []string{
	// Project resolution (paths.ResolveProjectDir priority 3).
	"CLAUDE_PROJECT_DIR",
	// Harness-native session ids; they outrank WIPNOTE_SESSION_ID.
	"CLAUDE_CODE_SESSION_ID",
	"CLAUDE_SESSION_ID",
	"CODEX_THREAD_ID",
	"GEMINI_SESSION_ID",
	"ANTIGRAVITY_SESSION_ID",
	// Harness detection markers (agent.DetectEnvHarness).
	"CLAUDECODE",
	"CLAUDE_CODE",
	"CLAUDE_CODE_ENTRYPOINT",
	// Env-file redirect — never write into the developer session's env file.
	"CLAUDE_ENV_FILE",
}

// ClearAmbientSession unsets every session-runtime variable inherited from the
// environment running the tests. Call it first thing in TestMain, before any
// package-level sandbox variables are set.
func ClearAmbientSession() {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if IsSessionVar(name) {
			_ = os.Unsetenv(name)
		}
	}
}

// IsSessionVar reports whether ClearAmbientSession clears name.
func IsSessionVar(name string) bool {
	for _, p := range clearedPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, n := range ClearedNames {
		if name == n {
			return true
		}
	}
	return false
}
