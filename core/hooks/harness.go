package hooks

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/harness"
)

// Harness identifies the AI coding harness that invoked this hook.
type Harness int

const (
	// HarnessClaude is the default harness (Claude Code via CloudEvent JSON).
	HarnessClaude Harness = iota
	// HarnessCodex is the OpenAI Codex CLI harness. Payload has a top-level
	// "hook_event_name" field, which distinguishes it from Claude's CloudEvent.
	HarnessCodex
	// HarnessGemini is the Google Gemini CLI harness. Payload has a top-level
	// "invocation_id" field and no "hook_event_name" field.
	HarnessGemini
	// HarnessAntigravity is the Google Antigravity CLI harness.
	HarnessAntigravity
)

// String returns a human-readable name for the harness.
func (h Harness) String() string {
	switch h {
	case HarnessCodex:
		return "codex"
	case HarnessGemini:
		return "gemini"
	case HarnessAntigravity:
		return "antigravity"
	default:
		return "claude"
	}
}

// harnessFromConfig bridges a *harness.HarnessConfig to the hooks.Harness int
// using the HooksHarness field populated in slice 1. Returns HarnessClaude when
// cfg is nil so callers always get a safe default.
func harnessFromConfig(cfg *harness.HarnessConfig) Harness {
	if cfg == nil {
		return HarnessClaude
	}
	return Harness(cfg.HooksHarness)
}

// DetectHarness is the exported entry point for harness detection. It calls
// detectHarness with the provided payload bytes. This is the function called
// from cmd/wipnote/hook.go.
func DetectHarness(payload []byte) Harness {
	return detectHarness(payload)
}

// DetectHarnessWithOverride returns the harness named by override when it is a
// known harness agent ID ("claude", "codex", "antigravity", ...), and falls
// back to DetectHarness otherwise.
//
// The override comes from the `--harness` flag that each generated non-Claude
// hooks.json passes on its hook command lines. That flag is the only signal
// that can't leak in from a parent process: CLAUDE_CODE_ENTRYPOINT and
// WIPNOTE_AGENT_ID are inherited, so `codex exec` started from inside a Claude
// Code session was classified as Claude and answered with Claude-shaped JSON
// (top-level "additionalContext") that Codex's strict output schema rejects,
// reporting "hook: SessionStart Failed" with no reason (GitHub issue #184).
func DetectHarnessWithOverride(payload []byte, override string) Harness {
	if h, ok := ParseHarnessName(override); ok {
		return h
	}
	return detectHarness(payload)
}

// ParseHarnessName maps a harness agent ID to its Harness. ok is false for an
// empty or unknown name.
func ParseHarnessName(name string) (Harness, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return HarnessClaude, false
	}
	if cfg := harness.GetByAgentID(name); cfg != nil {
		return Harness(cfg.HooksHarness), true
	}
	return HarnessClaude, false
}

// detectHarness examines the raw payload bytes and the process environment to
// determine the harness that sent them. The detection rules are (in priority order):
//
//  1. HarnessClaude: CLAUDE_CODE_ENTRYPOINT env var is set (Claude Code sets this
//     in every hook invocation; Codex and Gemini do not). This takes priority over
//     payload-based detection because Claude Code also sends "hook_event_name" in
//     its payloads, which previously caused false Codex classification.
//  2. HarnessGemini: WIPNOTE_AGENT_ID=gemini (set by `wipnote gemini` launcher).
//     This must be checked before payload fields because real Gemini hook payloads
//     do NOT include "invocation_id" — the Gemini hook schema uses hook_event_name
//     like Codex, making payload-only detection ambiguous.
//  3. HarnessCodex:  WIPNOTE_AGENT_ID=codex (set by `wipnote codex` launcher).
//  4. HarnessGemini: WIPNOTE_AGENT_ID absent AND top-level "invocation_id" present.
//  5. HarnessGemini: hook_event_name is a Gemini-native value (BeforeAgent, AfterAgent,
//     AfterModel, BeforeTool, AfterTool) — reliable payload-only discriminator when the
//     wipnote launcher is not used (i.e. `gemini` run directly).
//  6. HarnessCodex:  any other "hook_event_name" present.
//  7. HarnessClaude: default fallback.
func detectHarness(payload []byte) Harness {
	return detectHarnessWithEnv(payload, os.Getenv)
}

// detectHarnessWithEnv is the testable core of detectHarness. getenv is
// injected so tests can control environment without os.Setenv races.
func detectHarnessWithEnv(payload []byte, getenv func(string) string) Harness {
	// CLAUDE_CODE_ENTRYPOINT is set by Claude Code in every hook invocation.
	// Its presence is the most reliable signal that hooks are running inside
	// Claude Code — even when the payload also contains "hook_event_name"
	// (which Claude Code sends for all events, contra the previous assumption
	// that "hook_event_name" was Codex-exclusive).
	if getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return HarnessClaude
	}

	// WIPNOTE_AGENT_ID is set by the wipnote launcher before spawning the
	// harness process (buildGeminiAgentEnv sets WIPNOTE_AGENT_ID=gemini;
	// the Codex launcher sets WIPNOTE_AGENT_ID=codex). Use it as a reliable
	// env-based discriminator when payload-shape alone is ambiguous.
	//
	// This is necessary because real Gemini CLI hook payloads do NOT include
	// "invocation_id" — the official Gemini hook schema only has session_id,
	// cwd, hook_event_name, timestamp, and event-specific fields. Without this
	// check, every Gemini hook event is misclassified as HarnessCodex (because
	// hook_event_name IS present), causing agent_id='codex' to be written to
	// agent_events for Gemini sessions (the bug reported for session 8de1df19).
	if id := getenv("WIPNOTE_AGENT_ID"); id != "" {
		if cfg := harness.GetByAgentID(id); cfg != nil {
			return Harness(cfg.HooksHarness)
		}
	}

	if len(payload) == 0 {
		return HarnessClaude
	}

	// Unmarshal into a generic map for field-presence checks.
	var top map[string]any
	if err := json.Unmarshal(payload, &top); err != nil {
		return HarnessClaude
	}

	// Gemini: presence of "invocation_id" (future-proofing; real Gemini payloads omit it).
	if _, ok := top["invocation_id"]; ok {
		return HarnessGemini
	}

	// Gemini: hook_event_name values that Codex never emits. Gemini CLI uses its own
	// event naming convention (BeforeAgent, AfterAgent, AfterModel, BeforeTool, AfterTool)
	// rather than Codex's UserPromptSubmit / PreToolUse / PostToolUse names. This is the
	// only reliable payload-only discriminator when WIPNOTE_AGENT_ID is not set (i.e. when
	// `gemini` is run directly rather than via `wipnote gemini`).
	// Event names are read from the registry (HookEventNames field) rather than
	// hardcoded here — adding new Gemini events only requires updating registry_gemini.go.
	if name, _ := top["hook_event_name"].(string); name != "" {
		for _, cfg := range harness.All() {
			for _, n := range cfg.HookEventNames {
				if n == name {
					return harnessFromConfig(cfg)
				}
			}
		}
	}

	// Codex: presence of "hook_event_name" when not inside Claude Code.
	if _, ok := top["hook_event_name"]; ok {
		return HarnessCodex
	}

	return HarnessClaude
}
