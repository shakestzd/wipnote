// Package agent provides agent identity detection and session ID resolution
// for the wipnote CLI and hook subsystems.
//
// It is intentionally dependency-free (no internal imports) so that both the
// hook handlers and CLI commands can import it without creating import cycles.
package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Info describes the detected calling agent.
type Info struct {
	// ID is the agent identifier: "claude-code", "human", or a custom value
	// from WIPNOTE_AGENT_ID.
	ID string

	// Model is the model name from CLAUDE_MODEL, or empty if not set.
	Model string
}

// uuidPattern matches RFC 4122 UUID format (8-4-4-4-12).
// Pre-compiled at module load to avoid repeated compilation.
var uuidPattern = regexp.MustCompile(`([0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12})`)

// Detect returns the identity of the calling agent using env var priority:
//  1. WIPNOTE_AGENT_ID  — explicit override (e.g. "codex", "copilot")
//  2. CLAUDE_CODE=1 / CLAUDECODE=1 — running inside Claude Code → "claude-code"
//  3. fallback            — "human" (CLI user, not an AI agent)
//
// Model is always read from CLAUDE_MODEL (empty string if unset).
func Detect() Info {
	id := detectID()
	return Info{
		ID:    id,
		Model: os.Getenv("CLAUDE_MODEL"),
	}
}

func detectID() string {
	if v := os.Getenv("WIPNOTE_AGENT_ID"); v != "" {
		return v
	}
	// Claude Code 2.x sets CLAUDECODE=1 (no underscore); older builds set
	// CLAUDE_CODE=1. Accept either so agent_id never silently falls back to
	// "human" — that fallback collides with the agent_events CHECK constraint
	// `NOT (event_type='tool_call' AND agent_id='human' AND tool_name != 'UserQuery')`,
	// which silently rejects every Read/Bash/Edit insert and blinds the
	// research-first yolo guard.
	if os.Getenv("CLAUDE_CODE") != "" || os.Getenv("CLAUDECODE") != "" {
		return "claude-code"
	}
	return "human"
}

// ResolveSessionID returns the current session ID using a four-step fallback:
//  1. WIPNOTE_SESSION_ID env var (set by writeEnvVars via CLAUDE_ENV_FILE)
//  2. CLAUDE_SESSION_ID env var (normalised — Claude Code path-style IDs)
//  3. .wipnote/.active-session file in projectDir
//  4. Generated "cli-<pid>-<unix>" for plain CLI invocations
func ResolveSessionID(projectDir string) string {
	if v := os.Getenv("WIPNOTE_SESSION_ID"); v != "" {
		return v
	}
	if v := os.Getenv("CLAUDE_SESSION_ID"); v != "" {
		return NormaliseSessionID(v)
	}
	if projectDir != "" {
		if sid := readActiveSessionID(projectDir); sid != "" {
			return sid
		}
	}
	return fmt.Sprintf("cli-%d-%d", os.Getpid(), time.Now().Unix())
}

// NormaliseSessionID extracts a UUID from a path-style session_id that Claude
// Code sometimes provides for subagent sessions, e.g.:
//
//	/mock/claude-501/-Users-testuser-/550e8400-e29b-41d4-a716-446655440000
//
// If no UUID is found, or the input has no slash, the original string is
// returned unchanged.
func NormaliseSessionID(raw string) string {
	if raw == "" || !containsSlash(raw) {
		return raw
	}
	if m := uuidPattern.FindString(raw); m != "" {
		return m
	}
	return raw
}

// Harness name constants shared by DetectEnvHarness and its callers. They
// match the closed enum the SessionStart hook accepts for WIPNOTE_HARNESS.
const (
	HarnessClaude      = "claude"
	HarnessCodex       = "codex"
	HarnessGemini      = "gemini"
	HarnessAntigravity = "antigravity"
)

// DetectEnvHarness returns the harness the CURRENT process is running under,
// or "" when nothing in the environment identifies one.
//
// Precedence:
//  1. WIPNOTE_HARNESS — stamped by every wipnote launcher and authoritative,
//     because a nested launch (Codex task → `wipnote claude`) inherits the
//     parent harness's native variables (CODEX_THREAD_ID) alongside its own.
//  2. CLAUDE_CODE_ENTRYPOINT / CLAUDECODE / CLAUDE_CODE — set by Claude Code in
//     every hook invocation and Bash tool shell.
//  3. CODEX_THREAD_ID → GEMINI_SESSION_ID → ANTIGRAVITY_SESSION_ID — the
//     harness-native identity variables, which a plugin-only session (no
//     wipnote launcher, e.g. a Codex desktop task) exposes without any
//     WIPNOTE_HARNESS stamp (issue #148).
func DetectEnvHarness() string {
	switch h := strings.ToLower(strings.TrimSpace(os.Getenv("WIPNOTE_HARNESS"))); h {
	case HarnessClaude, HarnessCodex, HarnessGemini, HarnessAntigravity:
		return h
	}
	if os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" || os.Getenv("CLAUDECODE") != "" || os.Getenv("CLAUDE_CODE") != "" {
		return HarnessClaude
	}
	if strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" {
		return HarnessCodex
	}
	if strings.TrimSpace(os.Getenv("GEMINI_SESSION_ID")) != "" {
		return HarnessGemini
	}
	if strings.TrimSpace(os.Getenv("ANTIGRAVITY_SESSION_ID")) != "" {
		return HarnessAntigravity
	}
	return ""
}

// HarnessNativeEnvSessionID returns the live session/thread ID stamped by the
// current harness. The harness comes from DetectEnvHarness: the launcher's
// WIPNOTE_HARNESS stamp first, then the harness's own environment markers, so
// a plugin-only Codex desktop task (no wipnote launcher) still resolves its
// CODEX_THREAD_ID (issue #148). When a non-Claude harness is detected, the
// harness-native ID is preferred over WIPNOTE_SESSION_ID because the latter may
// be inherited (stale) from a parent Claude orchestrator shell (issue #144).
// Returns "" when no harness-native ID is found, or when running under Claude
// (where WIPNOTE_SESSION_ID is always current via writeEnvVars).
//
// Precedence: CODEX_THREAD_ID → GEMINI_SESSION_ID → ANTIGRAVITY_SESSION_ID
// depending on the detected harness.
func HarnessNativeEnvSessionID() string {
	switch DetectEnvHarness() {
	case HarnessCodex:
		return strings.TrimSpace(os.Getenv("CODEX_THREAD_ID"))
	case HarnessGemini:
		return strings.TrimSpace(os.Getenv("GEMINI_SESSION_ID"))
	case HarnessAntigravity:
		return strings.TrimSpace(os.Getenv("ANTIGRAVITY_SESSION_ID"))
	}
	return ""
}

func containsSlash(s string) bool {
	for _, c := range s {
		if c == '/' {
			return true
		}
	}
	return false
}

// activeSessionFile is the minimal JSON shape read from .wipnote/.active-session.
// We duplicate just the fields we need here to avoid importing internal/hooks
// (which would create an import cycle).
type activeSessionFile struct {
	SessionID string `json:"session_id"`
}

// readActiveSessionID reads the session_id field from .wipnote/.active-session.
// Returns empty string when the file doesn't exist or can't be parsed.
func readActiveSessionID(projectDir string) string {
	path := filepath.Join(projectDir, ".wipnote", ".active-session")
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var f activeSessionFile
	if err := json.Unmarshal(b, &f); err != nil {
		return ""
	}
	return f.SessionID
}
