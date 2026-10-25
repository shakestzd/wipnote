package hooks

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/eventsink"
)

// launchModeFile is the JSON structure written to .wipnote/.launch-mode by
// `wipnote claude`. It records how the current Claude session was started.
type launchModeFile struct {
	Mode      string `json:"mode"`
	PID       int    `json:"pid"`
	Timestamp string `json:"timestamp"`
}

var allowedHarnesses = map[string]struct{}{
	"claude":      {},
	"codex":       {},
	"gemini":      {},
	"antigravity": {},
}

// headCommit returns the short HEAD git hash, or empty string on failure.
func headCommit(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func normalizeHarness(raw string) string {
	h := strings.ToLower(strings.TrimSpace(raw))
	if _, ok := allowedHarnesses[h]; ok {
		return h
	}
	return ""
}

// resolveHarness returns the effective harness for the current session.
// Priority: WIPNOTE_HARNESS (launcher-stamped) → CLAUDE_CODE_ENTRYPOINT
// (Claude Code sets this in every hook invocation; its presence means the
// Claude launcher was used without stamping WIPNOTE_HARNESS, so default to
// "claude").
func resolveHarness() string {
	if h := normalizeHarness(os.Getenv("WIPNOTE_HARNESS")); h != "" {
		return h
	}
	if os.Getenv("CLAUDE_CODE_ENTRYPOINT") != "" {
		return "claude"
	}
	return ""
}

// activeSessionHarness picks the harness tag for the .active-session entry a
// SessionStart writes. The launcher stamp (WIPNOTE_HARNESS) is authoritative;
// otherwise the harness the hook runner parsed the payload under wins over the
// bare CLAUDE_CODE_ENTRYPOINT heuristic, because a Codex or Gemini process
// spawned from a Claude Bash tool inherits that variable while its own
// SessionStart payload is unmistakably non-Claude.
func activeSessionHarness(event *CloudEvent) string {
	if h := normalizeHarness(os.Getenv("WIPNOTE_HARNESS")); h != "" {
		return h
	}
	if event != nil {
		return event.Harness.String()
	}
	return resolveHarness()
}

// isSubagent returns true when env vars indicate this is a spawned subagent.
// Falls back to checking .active-session when env vars are absent (worktrees).
func isSubagent() bool {
	if os.Getenv("WIPNOTE_PARENT_SESSION") != "" {
		return os.Getenv("WIPNOTE_NESTING_DEPTH") != "0"
	}
	// Env vars not set — check if .active-session was written by the parent.
	// Worktree subagents get a fresh environment so WIPNOTE_PARENT_SESSION
	// won't be propagated, but the .active-session file is project-scoped.
	return false
}

// emitRosettaEvent writes a session_start NDJSON line correlating the
// launcher-minted OTel session ID (WIPNOTE_OTEL_SESSION_ID) with Claude Code's
// own session_id. This is the "Rosetta stone" record that lets the dashboard
// map a `claude --resume <id>` back to the originating wipnote session.
//
// The event is written only when wipnoteSID is set (i.e. WIPNOTE_OTEL_SESSION_ID
// was populated by the launcher, meaning the session was started via `wipnote
// claude`). If it is unset, or if the session directory cannot be created, the
// function returns silently.
func emitRosettaEvent(projectDir, wipnoteSID, claudeSessionID string) {
	if wipnoteSID == "" {
		return // not a launcher-managed session; skip silently
	}

	// Emit through the core eventsink boundary rather than importing otel
	// directly (feat-f87e93a6). The telemetry implementation is registered out
	// of band by internal/otel/eventsink; with no sink registered this is a
	// no-op, matching the best-effort semantics of the rosetta record.
	snk, err := eventsink.New(projectDir, wipnoteSID)
	if err != nil {
		debugLog(projectDir, "[session-start] rosetta: create event sink: %v", err)
		return
	}
	// Close flushes the in-memory buffer + fsyncs before the hook process exits.
	// Without this a single-event write stays in memory and is lost — the 2s
	// periodic ticker never fires in a short-lived hook process.
	defer func() {
		if err := snk.Close(); err != nil {
			debugLog(projectDir, "[session-start] rosetta: close event sink: %v", err)
		}
	}()

	ev := eventsink.Event{
		Harness:       "wipnote",
		SignalID:      "session-start-" + wipnoteSID,
		Kind:          eventsink.KindLog,
		CanonicalName: eventsink.CanonicalSessionStart,
		NativeName:    "session_start",
		Timestamp:     time.Now().UTC(),
		SessionID:     wipnoteSID,
		Attrs: map[string]any{
			"wipnote_sid":       wipnoteSID,
			"claude_session_id": claudeSessionID,
		},
	}
	if err := snk.EmitEvent(context.Background(), ev); err != nil {
		debugLog(projectDir, "[session-start] rosetta: write event: %v", err)
	}
}
