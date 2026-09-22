package hooks

import (
	"os"

	"github.com/shakestzd/wipnote/core/agent"
)

// Session-source labels reported by ResolveSessionID. They name WHERE the
// current session id came from so surfaces such as `wipnote who` can show the
// resolution path, and so a mismatch between two surfaces (issue #148: `who`
// and `bug start` bound to a launcher-created Claude session while the
// PreToolUse hook enforced against the originating Codex thread) is
// diagnosable instead of silent.
const (
	// SessionSourcePayload is the hook payload's session_id.
	SessionSourcePayload = "hook-payload"
	// SessionSourceHarnessEnv is the harness-native live id (CODEX_THREAD_ID,
	// GEMINI_SESSION_ID, ANTIGRAVITY_SESSION_ID, CLAUDE_CODE_SESSION_ID).
	SessionSourceHarnessEnv = "harness-env"
	// SessionSourceWipnoteEnv is the WIPNOTE_SESSION_ID env var.
	SessionSourceWipnoteEnv = "WIPNOTE_SESSION_ID"
	// SessionSourceActiveSession is the .wipnote/.active-session file.
	SessionSourceActiveSession = ".wipnote/.active-session"
	// SessionSourceNone means nothing resolved.
	SessionSourceNone = ""
)

// ResolveSessionID is the ONE resolver every "which session am I?" surface
// shares: EnvSessionID (hooks and CLI commands), `wipnote who`, and
// `wipnote <type> start` all delegate here, so they cannot disagree about the
// canonical current session.
//
// Resolution order, with the source label returned alongside the id:
//  1. eventSessionID — the hook payload's session_id, always correct for a
//     hook invocation.
//  2. The harness-native live id (agent.HarnessNativeEnvSessionID), preferred
//     over WIPNOTE_SESSION_ID because the latter may be inherited (stale) from
//     a parent orchestrator shell (issues #144, #125).
//  3. WIPNOTE_SESSION_ID.
//  4. .wipnote/.active-session — only when the entry was written by the same
//     harness this process runs under, or is untagged (legacy file). Any
//     harness's SessionStart overwrites that file, so a Codex task must never
//     adopt the entry a failed nested `wipnote claude` left behind (issue #148).
func ResolveSessionID(eventSessionID string) (id, source string) {
	if sid := agent.NormaliseSessionID(eventSessionID); sid != "" {
		return sid, SessionSourcePayload
	}
	if v := agent.HarnessNativeEnvSessionID(); v != "" {
		return v, SessionSourceHarnessEnv
	}
	if v := os.Getenv("WIPNOTE_SESSION_ID"); v != "" {
		return v, SessionSourceWipnoteEnv
	}
	cwd, _ := os.Getwd()
	projectDir := ResolveProjectDir(cwd, "")
	if projectDir == "" {
		return "", SessionSourceNone
	}
	as := ReadActiveSession(projectDir)
	if as == nil || as.SessionID == "" || !activeSessionMatchesCaller(as, agent.DetectEnvHarness()) {
		return "", SessionSourceNone
	}
	return as.SessionID, SessionSourceActiveSession
}

// activeSessionMatchesCaller reports whether an .active-session entry may be
// adopted by a process running under callerHarness. An untagged entry (written
// by an older wipnote) or an unknown caller keeps the pre-tag behaviour and is
// adopted; a tagged entry is adopted only by its own harness.
func activeSessionMatchesCaller(as *ActiveSessionData, callerHarness string) bool {
	if as == nil {
		return false
	}
	if as.Harness == "" || callerHarness == "" {
		return true
	}
	return as.Harness == callerHarness
}
