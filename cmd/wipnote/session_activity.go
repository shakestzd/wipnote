package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// sessionActivity is the cheap external liveness signal for one session:
// how many tool calls the hooks have recorded for it and when the last one
// happened. It exists so an orchestrator can tell an agent that never
// started (zero calls minutes after dispatch) from one that is working, and
// a finished-but-not-stopped agent from a stuck one (GH-#179).
type sessionActivity struct {
	// Calls is the number of tool-call entries recorded for the session.
	Calls int
	// LastCall is the most recent recorded activity; zero when none.
	LastCall time.Time
	// Source names where the numbers came from: "hooks" for the session HTML
	// activity log written by PostToolUse, "otel" for the events.ndjson
	// signal stream mtime (last activity only, no call count), "" for none.
	Source string
}

// activityLogEntryRe matches one PostToolUse entry in a session HTML activity
// log, capturing its data-ts timestamp. AppendEventToSessionHTML always writes
// data-ts as the first attribute, so this anchors on the exact shape it emits.
var activityLogEntryRe = regexp.MustCompile(`<li data-ts="([^"]*)"`)

// sessionActivityFor derives the activity for sessionID from whatever
// per-session event data exists under <wipnoteDir>/sessions/:
//
//   - <id>.html         hook-written activity log (one <li> per tool call)
//   - <id>/events.ndjson OTel signal stream; only its mtime is used, as a
//     last-activity fallback when the HTML log has no entries
//
// Both are ephemeral and gitignored, so a session whose data was pruned or
// archived reports no activity rather than a wrong number.
func sessionActivityFor(wipnoteDir, sessionID string) sessionActivity {
	sessionsDir := filepath.Join(wipnoteDir, "sessions")
	if act, ok := activityFromSessionHTML(filepath.Join(sessionsDir, sessionID+".html")); ok {
		return act
	}
	if info, err := os.Stat(filepath.Join(sessionsDir, sessionID, "events.ndjson")); err == nil {
		return sessionActivity{LastCall: info.ModTime().UTC(), Source: "otel"}
	}
	return sessionActivity{}
}

// activityFromSessionHTML counts activity-log entries and finds the latest
// data-ts. ok is false when the file is missing or holds no entries.
func activityFromSessionHTML(path string) (sessionActivity, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sessionActivity{}, false
	}
	content := string(data)
	if idx := strings.Index(content, "<section data-activity-log>"); idx >= 0 {
		content = content[idx:]
	}
	var act sessionActivity
	for _, m := range activityLogEntryRe.FindAllStringSubmatch(content, -1) {
		act.Calls++
		if t, perr := time.Parse(time.RFC3339, m[1]); perr == nil && t.After(act.LastCall) {
			act.LastCall = t.UTC()
		}
	}
	if act.Calls == 0 {
		return sessionActivity{}, false
	}
	act.Source = "hooks"
	return act, true
}

// callsColumn renders the CALLS cell: the count from the hook log, or "-"
// when no hook data exists for the session.
func (a sessionActivity) callsColumn() string {
	if a.Source != "hooks" {
		return "-"
	}
	return fmt.Sprintf("%d", a.Calls)
}

// lastCallColumn renders the LAST CALL cell as time elapsed since the last
// recorded activity ("4m12s ago"), or "-" when nothing was recorded.
func (a sessionActivity) lastCallColumn(now time.Time) string {
	if a.LastCall.IsZero() {
		return "-"
	}
	since := now.Sub(a.LastCall)
	if since < 0 {
		since = 0
	}
	return fmtDuration(since) + " ago"
}

// describeLastCall renders the `session show` detail: absolute timestamp
// plus elapsed time, with the source named so the reader knows whether the
// figure is a tool call or only a signal-stream write.
func (a sessionActivity) describeLastCall(now time.Time) string {
	if a.LastCall.IsZero() {
		return "- (no tool calls recorded)"
	}
	abs := a.LastCall.Local().Format("2006-01-02 15:04:05")
	if a.Source == "otel" {
		return fmt.Sprintf("%s (%s; from signal stream, not a tool call)", abs, a.lastCallColumn(now))
	}
	return fmt.Sprintf("%s (%s)", abs, a.lastCallColumn(now))
}
