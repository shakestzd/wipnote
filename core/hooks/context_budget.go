package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

// Per-hook caps on model-visible additionalContext, in characters. Claude Code
// delivers hookSpecificOutput.additionalContext verbatim (and spills anything
// over 10,000 chars to a file), so every byte injected on a hot path such as
// PreToolUse is paid on every tool call. Tool-event hooks get a small budget;
// SessionStart/UserPromptSubmit carry real guidance and get a larger one that
// stays under Claude Code's own 10,000-char spill threshold.
const (
	toolEventContextCap   = 800
	promptEventContextCap = 8000
	contextTruncMarker    = "…[truncated]"
)

// contextCapForEvent returns the additionalContext cap for a hook event.
func contextCapForEvent(event string) int {
	switch event {
	case "PreToolUse", "PostToolUse", "PostToolUseFailure":
		return toolEventContextCap
	default:
		return promptEventContextCap
	}
}

// capContext truncates s to at most max runes, appending a marker.
func capContext(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	keep := max - len([]rune(contextTruncMarker))
	if keep < 0 {
		keep = 0
	}
	return string(r[:keep]) + contextTruncMarker
}

// claimSessionOnce reports whether key has NOT yet been shown in this session,
// recording it durably so later hook processes (each hook is a fresh process)
// see it. It reuses the per-session marker directory
// .wipnote/sessions/<session-id>/ that already holds .permission-mode and the
// liveness anchors. O_EXCL makes the claim atomic across concurrent hooks.
//
// With no session ID or state dir there is nothing to key on, so it returns
// true (show). If the marker cannot be written it returns false: repeating an
// advisory on every tool call is the failure this exists to prevent.
func claimSessionOnce(wipnoteDir, sessionID, key string) bool {
	if wipnoteDir == "" || sessionID == "" || key == "" {
		return true
	}
	dir := filepath.Join(wipnoteDir, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	name := ".once-" + strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, key)
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
