package hooks

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/agent"
)

// Canonical per-session tool-call log.
//
// The hot PreToolUse hook opens an EMPTY in-memory projection
// (OpenHookDBReadOnly → db.OpenEphemeralProjectionTablesOnly), so every guard
// that counted agent_events rows to decide "did this session already research /
// screenshot / touch UI files" reads zero and degrades to its fail-open branch.
// The guards are therefore inert, not wrong (bug-a3b17225).
//
// recordEventAndAllow already writes the authoritative row through the daemon;
// it now ALSO appends a tiny canonical line here, next to the session's other
// canonical artifacts, so the read-only hook path has a durable file source for
// the same facts. Only the classification-relevant fields are kept, each
// truncated, and the file stops growing at canonicalToolLogMaxBytes — a hook
// must never be the reason a session's disk fills up.
//
// Fail-open is preserved end to end: a missing, empty or unreadable log means
// "cannot verify" and the guards allow, exactly as they do today. The log only
// ever turns an inert guard back ON when it positively records the session's
// tool calls.
const (
	canonicalToolLogName = "tool-events.ndjson"
	// canonicalToolLogMaxBytes caps the file: past this size appends stop.
	// Early events are kept rather than rotated away because the facts the
	// guards ask about (research happened, a screenshot was taken) are
	// established early in a session and must not expire mid-session.
	canonicalToolLogMaxBytes = 512 << 10
	// canonicalToolLogTailBytes bounds a single read on the hot hook path.
	canonicalToolLogTailBytes = 512 << 10
	// canonicalToolFieldMax truncates each recorded string field.
	canonicalToolFieldMax = 400
)

// canonicalToolEvent is one recorded tool call: the minimum the guards classify
// on. Field names are short because every tool call in every session pays for
// them.
type canonicalToolEvent struct {
	Tool    string `json:"t"`
	Agent   string `json:"a,omitempty"`
	Summary string `json:"s,omitempty"`
	Input   string `json:"i,omitempty"`
}

// canonicalToolLogPath is the log for one session.
func canonicalToolLogPath(projectDir, sessionID string) string {
	return filepath.Join(projectDir, ".wipnote", "sessions", sessionID, canonicalToolLogName)
}

// appendCanonicalToolEvent records one tool call. Best-effort and silent: the
// authoritative write is the daemon-routed agent_events INSERT, and a hook must
// never fail because an observability append failed.
func appendCanonicalToolEvent(projectDir, sessionID string, ev canonicalToolEvent) {
	if projectDir == "" || sessionID == "" || ev.Tool == "" {
		return
	}
	path := canonicalToolLogPath(projectDir, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() >= canonicalToolLogMaxBytes {
		return
	}
	ev.Tool = truncateField(ev.Tool)
	ev.Agent = truncateField(ev.Agent)
	ev.Summary = truncateField(ev.Summary)
	ev.Input = truncateField(ev.Input)
	line, err := json.Marshal(ev)
	if err != nil {
		return
	}
	// One Write of a short record under O_APPEND: atomic enough that concurrent
	// hook processes cannot interleave halves of a line on a local filesystem.
	_, _ = f.Write(append(line, '\n'))
}

func truncateField(s string) string {
	if len(s) <= canonicalToolFieldMax {
		return s
	}
	return s[:canonicalToolFieldMax]
}

// readCanonicalToolEvents parses the bounded tail of one session's log.
// ok is false when nothing could be read — no log, empty log, or no parsable
// line — which callers must treat as "cannot verify" and fail open.
func readCanonicalToolEvents(projectDir, sessionID string) (events []canonicalToolEvent, ok bool) {
	if projectDir == "" || sessionID == "" {
		return nil, false
	}
	f, err := os.Open(canonicalToolLogPath(projectDir, sessionID))
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return nil, false
	}
	offset := int64(0)
	if info.Size() > canonicalToolLogTailBytes {
		offset = info.Size() - canonicalToolLogTailBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false
	}
	buf, err := io.ReadAll(io.LimitReader(f, canonicalToolLogTailBytes))
	if err != nil {
		return nil, false
	}
	lines := strings.Split(string(buf), "\n")
	if offset > 0 && len(lines) > 0 {
		lines = lines[1:] // the seek landed mid-record
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev canonicalToolEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil || ev.Tool == "" {
			continue
		}
		events = append(events, ev)
	}
	return events, len(events) > 0
}

// canonicalToolEventScope is the set of sessions whose logs answer for
// sessionID: the session itself plus its family root, which is where a
// Codex-style subagent's orchestrator recorded its own tool calls.
func canonicalToolEventScope(projectDir, sessionID string) []string {
	scope := []string{sessionID}
	if root := agent.SessionFamilyFor(projectDir, sessionID); root != "" && root != sessionID {
		scope = append(scope, root)
	}
	return scope
}

// canonicalToolEventsInScope reads every in-scope session's log. ok is false
// when NO scoped session has a readable log — a recording gap.
func canonicalToolEventsInScope(projectDir, sessionID string) (events []canonicalToolEvent, ok bool) {
	for _, sid := range canonicalToolEventScope(projectDir, sessionID) {
		if evs, found := readCanonicalToolEvents(projectDir, sid); found {
			events = append(events, evs...)
			ok = true
		}
	}
	return events, ok
}

// canonicalAnySeen reports whether any in-scope recorded tool call satisfies
// match. ok is false on a recording gap; callers fail open on !ok.
func canonicalAnySeen(projectDir, sessionID string, match func(canonicalToolEvent) bool) (seen, ok bool) {
	events, ok := canonicalToolEventsInScope(projectDir, sessionID)
	if !ok {
		return false, false
	}
	for _, ev := range events {
		if match(ev) {
			return true, true
		}
	}
	return false, true
}

// researchToolNames mirrors the tool_name IN (...) list in buildResearchQuery.
// Keep the two in lockstep: they answer the same question from two sources.
var researchToolNames = map[string]bool{
	"Read": true, "Grep": true, "Glob": true,
	"WebSearch": true, "WebFetch": true,
	"read_file": true, "grep_search": true, "glob": true, "list_directory": true,
	"web_fetch": true, "web_search": true, "google_web_search": true,
}

// researchShellPrefixes mirrors the input_summary LIKE list in
// buildResearchQuery: shell commands that read rather than mutate.
var researchShellPrefixes = []string{
	"ls ", "find ", "cat ", "grep ", "head ", "tail ", "stat ",
	"gh ", "curl ", "wipnote sh ", "wipnote search ",
}

// isCanonicalResearchEvent classifies a recorded tool call as research.
func isCanonicalResearchEvent(ev canonicalToolEvent) bool {
	if researchToolNames[ev.Tool] {
		return true
	}
	if !isShellTool(ev.Tool) {
		return false
	}
	summary := strings.TrimSpace(ev.Summary)
	if summary == "ls" {
		return true
	}
	for _, prefix := range researchShellPrefixes {
		if strings.HasPrefix(summary, prefix) {
			return true
		}
	}
	// Read-only sed mirrors buildResearchQuery's `sed %` AND NOT `sed -i%`
	// AND NOT `sed --in-place%` clause: a substitution printed to stdout is
	// research, an in-place edit is not.
	if strings.HasPrefix(summary, "sed ") &&
		!strings.HasPrefix(summary, "sed -i") &&
		!strings.HasPrefix(summary, "sed --in-place") {
		return true
	}
	return false
}

// uiEditExtensions mirrors the input_summary LIKE list in the UI-validation
// guard's "were UI files touched this session" query.
var uiEditExtensions = []string{".html", ".css", ".js", ".ts", ".tsx", ".vue", ".svelte"}

// isCanonicalUIEditEvent reports whether a recorded tool call edited a UI file.
// .wipnote/ HTML is work-item data, not UI, and is excluded exactly as the SQL
// query excludes it.
func isCanonicalUIEditEvent(ev canonicalToolEvent) bool {
	switch ev.Tool {
	case "Write", "Edit", "MultiEdit":
	default:
		return false
	}
	target := ev.Summary + " " + ev.Input
	if strings.Contains(target, ".wipnote/") {
		return false
	}
	for _, ext := range uiEditExtensions {
		if strings.Contains(target, ext) {
			return true
		}
	}
	return false
}

// isCanonicalScreenshotEvent mirrors the UI-validation guard's screenshot
// detection: a dedicated screenshot tool, or an "action":"screenshot"
// discriminator nested in the input of a batch-style MCP tool.
func isCanonicalScreenshotEvent(ev canonicalToolEvent) bool {
	if strings.Contains(strings.ToLower(ev.Tool), "screenshot") {
		return true
	}
	joined := strings.ReplaceAll(ev.Summary+ev.Input, " ", "")
	return strings.Contains(joined, `"action":"screenshot"`)
}
