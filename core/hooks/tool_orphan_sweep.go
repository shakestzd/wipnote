package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Orphaned tool-call sweep over the canonical tool log (feat-174dc958).
//
// PreToolUse writes a start line carrying the call's id and a timestamp;
// PostToolUse writes a completion line under the same id. A start with no
// completion is a tool call that never finished: the harness crashed, was
// killed, or the session ended mid-call. SessionStart records such a call as
// "aborted" once it is over OrphanThreshold old, or at once when its session
// recorded an end after the call started. The record is a completion line
// (status aborted) in the same log, which is also what makes the sweep
// idempotent, plus an aborted entry in the session HTML activity log.
//
// The sweep reads only session folders whose tool log changed within
// orphanScanWindow, and skips a log at its 512 KiB cap: past the cap
// completion lines are not written, so an unmatched start there proves
// nothing.

// OrphanThreshold is the age a started tool call must reach, with no
// completion, before the sweep records it as aborted.
const OrphanThreshold = 5 * time.Minute

// orphanScanWindow bounds which session folders the sweep reads: only those
// whose tool log was written within this window.
const orphanScanWindow = 24 * time.Hour

// SessionStartSweepCap bounds how many aborted calls one SessionStart records.
const SessionStartSweepCap = 8

// toolLogNow is the tool-log clock; tests set it to backdate a start line.
var toolLogNow = time.Now

// toolStart is one start line that carries an id.
type toolStart struct {
	id, tool string
	at       time.Time
}

// SweepOrphanedToolCalls records every orphaned tool call in recently active
// sessions other than currentSessionID, at most limit of them (limit <= 0 is
// unlimited). It returns how many it recorded.
func SweepOrphanedToolCalls(projectDir, currentSessionID string, now time.Time, limit int) int {
	recorded := 0
	for _, sid := range recentToolLogSessions(projectDir, now) {
		if sid == currentSessionID {
			continue
		}
		budget := 0
		if limit > 0 {
			if recorded >= limit {
				break
			}
			budget = limit - recorded
		}
		recorded += sweepSessionToolLog(projectDir, sid, now, budget)
	}
	return recorded
}

// recentToolLogSessions lists the sessions whose tool log changed within
// orphanScanWindow of now.
func recentToolLogSessions(projectDir string, now time.Time) []string {
	if projectDir == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(projectDir, ".wipnote", "sessions"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := os.Stat(canonicalToolLogPath(projectDir, e.Name()))
		if err == nil && now.Sub(info.ModTime()) <= orphanScanWindow {
			out = append(out, e.Name())
		}
	}
	return out
}

// sweepSessionToolLog records one session's orphaned calls, at most budget of
// them (budget <= 0 is unlimited).
func sweepSessionToolLog(projectDir, sessionID string, now time.Time, budget int) int {
	path := canonicalToolLogPath(projectDir, sessionID)
	info, err := os.Stat(path)
	if err != nil || info.Size() >= canonicalToolLogMaxBytes {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	starts, done := parseToolLogPairs(data)
	var endedAt time.Time
	if note, ok := ReadSessionEndNote(projectDir, sessionID); ok {
		endedAt = note.EndTime()
	}
	recorded := 0
	for _, s := range starts {
		if budget > 0 && recorded >= budget {
			break
		}
		if done[s.id] || !toolCallOrphaned(s, now, endedAt) {
			continue
		}
		if recordAbortedToolCall(projectDir, sessionID, s) {
			recorded++
		}
	}
	return recorded
}

// toolCallOrphaned reports whether an unmatched start counts as aborted: it
// is over OrphanThreshold old, or its session ended after it started.
func toolCallOrphaned(s toolStart, now, endedAt time.Time) bool {
	if now.Sub(s.at) > OrphanThreshold {
		return true
	}
	return !endedAt.IsZero() && endedAt.After(s.at)
}

// toolLogLine is the union of a start line and a completion line.
type toolLogLine struct {
	Tool string `json:"t"`
	ID   string `json:"id"`
	TS   int64  `json:"ts"`
	Done string `json:"done"`
}

// parseToolLogPairs reads a tool log into its id-carrying start lines (in
// order) and the set of ids that have a completion line.
func parseToolLogPairs(data []byte) ([]toolStart, map[string]bool) {
	var starts []toolStart
	done := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		var rec toolLogLine
		if strings.TrimSpace(line) == "" || json.Unmarshal([]byte(line), &rec) != nil || rec.ID == "" {
			continue
		}
		switch {
		case rec.Done != "":
			done[rec.ID] = true
		case rec.Tool != "" && rec.TS > 0:
			starts = append(starts, toolStart{id: rec.ID, tool: rec.Tool, at: time.Unix(rec.TS, 0).UTC()})
		}
	}
	return starts, done
}

// recordAbortedToolCall writes the aborted completion line and, when that
// lands, the aborted session HTML entry.
func recordAbortedToolCall(projectDir, sessionID string, s toolStart) bool {
	if !appendToolCompletion(projectDir, sessionID, s.id, "aborted") {
		return false
	}
	AppendEventToSessionHTML(projectDir, sessionID, SessionEvent{
		Timestamp: s.at,
		ToolName:  s.tool,
		Success:   false,
		EventID:   s.id,
		Summary:   "[aborted] " + s.tool + " never completed",
		Status:    "aborted",
		Reason:    "no-post-hook",
	})
	return true
}

// appendToolCompletion writes the completion line for id. It respects the
// log's size cap and reports whether the line was written.
func appendToolCompletion(projectDir, sessionID, id, status string) bool {
	if projectDir == "" || sessionID == "" || id == "" {
		return false
	}
	f, err := os.OpenFile(canonicalToolLogPath(projectDir, sessionID), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || info.Size() >= canonicalToolLogMaxBytes {
		return false
	}
	line, err := json.Marshal(struct {
		ID   string `json:"id"`
		Done string `json:"done"`
	}{id, status})
	if err != nil {
		return false
	}
	_, err = f.Write(append(line, '\n'))
	return err == nil
}
