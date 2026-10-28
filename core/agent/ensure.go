package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// EnsureSession records the current session in the canonical session ledger.
func EnsureSession(_ any, projectDir string) (string, error) {
	return EnsureSessionWithTimeout(nil, projectDir, 0)
}

// EnsureSessionRouted keeps the old split-handle call shape while ignoring the
// obsolete database handles. The ledger write is idempotent and durable.
func EnsureSessionRouted(_ any, _ any, projectDir, _ string, timeout time.Duration) (string, error) {
	return EnsureSessionWithTimeout(nil, projectDir, timeout)
}

// EnsureSessionWithTimeout records the current session in sessions-ledger.html
// and writes .active-session for hook consumers. timeout is accepted for
// compatibility with old database-bound callers.
func EnsureSessionWithTimeout(_ any, projectDir string, _ time.Duration) (string, error) {
	sessionID := ResolveSessionID(projectDir)
	info := Detect()

	if strings.HasPrefix(sessionID, "cli-") {
		return sessionID, nil
	}

	if err := ensureLedgerRow(projectDir, sessionID); err != nil {
		return sessionID, err
	}

	writeEnsuredActiveSession(sessionID, projectDir, info.ID)
	os.Setenv("WIPNOTE_SESSION_ID", sessionID) //nolint:errcheck
	return sessionID, nil
}

// ensureLedgerRow records sessionID in the ledger when no row exists yet. This
// is a LATE writer: SessionStart normally wrote the row already, so this only
// fills a row SessionStart skipped. The row takes the harness the environment
// names (DetectEnvHarness) — the same tag SessionStart writes — never the agent
// identity, whose "human" fallback misfiled a real session under a harness
// that does not exist (bug-5333ea49). With no harness known (a plain shell or
// a delegate script that resolved the session from .active-session) the row is
// left out: SessionStart, which knows the harness, owns it.
//
// It writes through Store.Ensure, never Open: this caller cannot tell a live
// session from one it outlived, so it must not record a resume of a closed
// row (bug-2a96e9cb).
func ensureLedgerRow(projectDir, sessionID string) error {
	harness := DetectEnvHarness()
	if harness == "" {
		return nil
	}
	record := sessionledger.Record{
		SessionID:  sessionID,
		Harness:    harness,
		ProjectDir: paths.NormalizeProjectDir(projectDir),
		StartedAt:  time.Now().UTC(),
	}
	_, err := sessionledger.NewStore(filepath.Join(projectDir, ".wipnote")).Ensure(record)
	return err
}

// ensuredActiveSession is the JSON shape written to .wipnote/.active-session
// by EnsureSession. It mirrors hooks.ActiveSessionData to keep the format
// consistent without creating an import dependency.
type ensuredActiveSession struct {
	SessionID     string  `json:"session_id"`
	ParentSession string  `json:"parent_session,omitempty"`
	ParentAgent   string  `json:"parent_agent,omitempty"`
	NestingDepth  int     `json:"nesting_depth"`
	ProjectDir    string  `json:"project_dir,omitempty"`
	GitRemoteURL  string  `json:"git_remote_url,omitempty"`
	Timestamp     float64 `json:"timestamp"`
}

// writeEnsuredActiveSession writes minimal session context to
// .wipnote/.active-session. Errors are silently ignored — this is a
// best-effort propagation mechanism; hook handlers fall back gracefully.
func writeEnsuredActiveSession(sessionID, projectDir, agentID string) {
	if projectDir == "" {
		return
	}
	data := ensuredActiveSession{
		SessionID:     sessionID,
		ParentSession: sessionID,
		ParentAgent:   agentID,
		NestingDepth:  0,
		ProjectDir:    projectDir,
		GitRemoteURL:  paths.GetGitRemoteURL(projectDir),
		Timestamp:     float64(time.Now().UnixNano()) / 1e9,
	}
	b, err := json.Marshal(data)
	if err != nil {
		return
	}
	path := filepath.Join(projectDir, ".wipnote", ".active-session")
	_ = os.WriteFile(path, b, 0o644)
}
