package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
)

// ActiveSessionData is the JSON structure written to .wipnote/.active-session
// as a fallback propagation mechanism when CLAUDE_ENV_FILE is unset (worktree
// subagents). All fields mirror what writeEnvVars() exports via CLAUDE_ENV_FILE.
type ActiveSessionData struct {
	SessionID     string  `json:"session_id"`
	ParentSession string  `json:"parent_session,omitempty"`
	ParentAgent   string  `json:"parent_agent,omitempty"`
	NestingDepth  int     `json:"nesting_depth"`
	ProjectDir    string  `json:"project_dir,omitempty"`
	GitRemoteURL  string  `json:"git_remote_url,omitempty"`
	Timestamp     float64 `json:"timestamp"`
	// Harness names the harness whose SessionStart wrote this entry
	// ("claude", "codex", "gemini", "antigravity"). Every harness's
	// SessionStart overwrites the same file, so a reader running under a
	// different harness must not adopt the entry (issue #148). Empty on files
	// written by older wipnote versions; readers treat those as untagged and
	// keep the pre-tag behaviour.
	Harness string `json:"harness,omitempty"`
}

// WriteActiveSessionForHarness is WriteActiveSession with an explicit harness
// tag (see ActiveSessionData.Harness).
//
// Writes are atomic (write-to-temp + rename) so concurrent readers never see
// a torn/empty file, and concurrent writers cannot corrupt each other
// (bug-d2d3fb3f: parallel agents stomped .active-session).
func WriteActiveSessionForHarness(sessionID, projectDir, harness string) {
	if projectDir == "" {
		return
	}
	data := ActiveSessionData{
		SessionID:     sessionID,
		ParentSession: sessionID,
		ParentAgent:   "claude-code",
		NestingDepth:  0,
		ProjectDir:    projectDir,
		GitRemoteURL:  paths.GetGitRemoteURL(projectDir),
		Timestamp:     float64(time.Now().UnixNano()) / 1e9,
		Harness:       normalizeHarness(harness),
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	dir := filepath.Join(projectDir, ".wipnote")
	target := filepath.Join(dir, ".active-session")
	tmp, err := os.CreateTemp(dir, ".active-session.tmp-*")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		_ = os.Remove(tmpPath)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return
	}
	_ = os.Chmod(tmpPath, 0o644)
	if err := os.Rename(tmpPath, target); err != nil {
		_ = os.Remove(tmpPath)
	}
}

// ReadActiveSession reads session context from .wipnote/.active-session.
// Returns nil when the file doesn't exist or can't be parsed.
func ReadActiveSession(projectDir string) *ActiveSessionData {
	if projectDir == "" {
		return nil
	}
	path := filepath.Join(projectDir, ".wipnote", ".active-session")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var data ActiveSessionData
	if err := json.Unmarshal(b, &data); err != nil {
		return nil
	}
	return &data
}

// writeEnvVars appends session context exports to CLAUDE_ENV_FILE and always
// writes .wipnote/.active-session as a backup. The .active-session file
// ensures downstream hooks can resolve the session ID even when CLAUDE_ENV_FILE
// is unavailable (YOLO mode, worktree subagents, plugin-dir launches).
func writeEnvVars(sessionID, projectDir, harness string) {
	// Always write .active-session as backup — prevents stale session IDs.
	WriteActiveSessionForHarness(sessionID, projectDir, harness)

	envFile := os.Getenv("CLAUDE_ENV_FILE")
	if envFile == "" {
		debugLog(projectDir, "[wipnote] CLAUDE_ENV_FILE unset — using .active-session only (session_id=%s)", sessionID)
		return
	}
	if err := os.MkdirAll(filepath.Dir(envFile), 0o755); err != nil {
		debugLog(projectDir, "[wipnote] failed to create CLAUDE_ENV_FILE dir %s: %v", filepath.Dir(envFile), err)
		return
	}
	f, err := os.OpenFile(envFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		debugLog(projectDir, "[wipnote] failed to open CLAUDE_ENV_FILE %s: %v", envFile, err)
		return
	}
	defer f.Close()

	lines := []string{
		"export WIPNOTE_SESSION_ID=" + sessionID,
		"export WIPNOTE_PARENT_SESSION=" + sessionID,
		"export WIPNOTE_PARENT_AGENT=claude-code",
		"export WIPNOTE_NESTING_DEPTH=0",
	}
	if projectDir != "" {
		lines = append(lines, "export CLAUDE_PROJECT_DIR="+projectDir)
	}
	f.WriteString(strings.Join(lines, "\n") + "\n")
}
