package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Compaction continuity (feat-9bc8e611): when a harness compacts a session's
// context, the agent should keep its work item state. PreCompact snapshots the
// resume brief and steers the summary; SessionStart with source=compact
// re-injects the brief (session_start.go); PostCompact records the summary.
//
// Payloads, verified against https://code.claude.com/docs/en/hooks and
// https://learn.chatgpt.com/docs/hooks: PreCompact carries trigger and
// custom_instructions, PostCompact carries trigger and compact_summary.

const (
	compactBriefFile   = "compact-brief.md"
	compactionsLogFile = "compactions.ndjson"

	// preCompactMaxChars keeps the summary instructions under Claude Code's
	// 10,000 character cap on PreCompact stdout.
	preCompactMaxChars = 4000
)

// PreCompact snapshots the resume brief to the session folder and returns the
// instructions for the compaction summary. It returns "" when the session has
// no active work item. It never blocks compaction: every failure is logged and
// the instructions are still returned.
func PreCompact(event *CloudEvent, projectDir string) string {
	sessionID := resolveSessionIDWithHarness(event)
	st := loadResumeState(projectDir, sessionID)
	if st == nil {
		return ""
	}
	if err := writeSessionFile(projectDir, sessionID, compactBriefFile, []byte(renderResumeBrief(st))); err != nil {
		LogError("pre-compact", sessionID, fmt.Sprintf("write brief snapshot: %v", err))
	}
	return compactInstructions(st)
}

// compactInstructions tells the summariser what to keep. Claude Code uses the
// PreCompact hook's plain stdout as instructions for the summary.
func compactInstructions(st *resumeState) string {
	item := st.ID
	if st.Title != "" {
		item += " — " + clip(st.Title, 120)
	}
	lines := []string{
		"Keep this work item state in the summary:",
		"- Active work item: " + item,
	}
	if next := st.nextOpenStep(); next != "" {
		lines = append(lines, "- Current step: "+clip(next, 200))
	}
	lines = append(lines,
		"- The decisions made so far, and the reason for each",
		"- The files changed so far",
		"- The next step to take",
		fmt.Sprintf("After compaction the agent can run `wipnote context-pack %s` for the full brief.", st.ID),
	)
	return clip(strings.Join(lines, "\n"), preCompactMaxChars)
}

// compactionRecord is one line of compactions.ndjson.
type compactionRecord struct {
	Timestamp      string `json:"timestamp"`
	Trigger        string `json:"trigger"`
	CompactSummary string `json:"compact_summary"`
}

// PostCompact appends the compaction's trigger and summary to the session's
// compactions.ndjson. It is side effects only and never fails the hook.
func PostCompact(event *CloudEvent, projectDir string) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		return &HookResult{}, nil
	}
	row, err := json.Marshal(compactionRecord{
		Timestamp:      time.Now().UTC().Format(time.RFC3339),
		Trigger:        event.Trigger,
		CompactSummary: event.CompactSummary,
	})
	if err == nil {
		err = appendSessionFile(projectDir, sessionID, compactionsLogFile, append(row, '\n'))
	}
	if err != nil {
		LogError("post-compact", sessionID, fmt.Sprintf("record compaction: %v", err))
	}
	return &HookResult{}, nil
}

// sessionFilePath is where a session's own files live, or an error for a
// session id that would leave the sessions folder.
func sessionFilePath(projectDir, sessionID, name string) (string, error) {
	if sessionID == "" || strings.ContainsAny(sessionID, `/\`) || sessionID == "." || sessionID == ".." {
		return "", fmt.Errorf("unusable session id %q", sessionID)
	}
	dir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// writeSessionFile replaces a file in the session folder, so a reader never
// sees a half-written snapshot.
func writeSessionFile(projectDir, sessionID, name string, data []byte) error {
	path, err := sessionFilePath(projectDir, sessionID, name)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// appendSessionFile appends data to a file in the session folder.
func appendSessionFile(projectDir, sessionID, name string, data []byte) error {
	path, err := sessionFilePath(projectDir, sessionID, name)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
