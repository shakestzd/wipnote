package hooks

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/agent"
)

// isYoloFromEvent checks the CloudEvent permission_mode field first (live
// state from Claude Code), falling back to a SQLite DB lookup.
// Fast-path: WIPNOTE_YOLO=1 is set only by the wipnote launcher on an explicit
// --yolo launch (e.g. `wipnote codex --yolo`). It is harness-agnostic and has
// no false-positive path because nothing else sets this variable.
func isYoloFromEvent(event *CloudEvent, wipnoteDir string) bool {
	if os.Getenv("WIPNOTE_YOLO") == "1" {
		return true
	}
	if event.PermissionMode == "bypassPermissions" {
		return true
	}
	// If Claude Code reports a non-bypass mode, trust it.
	if event.PermissionMode != "" {
		return false
	}
	// Fallback: the session's last recorded permission_mode, persisted by the
	// ConfigChange hook handler.
	return isYoloFromRecordedMode(wipnoteDir, event.SessionID)
}

// isYoloWithInheritance checks YOLO mode for the current session and, when the
// current session has no YOLO marker of its own, walks the parent-session chain
// to check whether any ancestor session is in YOLO posture.
//
// Top-level vs subagent semantics (bug-0ed4e469):
//   - isYoloFromEvent's bypassPermissions fast-path always wins for both.
//   - For a TOP-LEVEL session (isSubagent=false), an explicit non-empty,
//     non-bypass permission_mode is a deliberate user declaration of non-YOLO
//     posture and is honored: the chain walk is skipped and we return false.
//   - For a SUBAGENT (isSubagent=true), its own reported permission_mode is NOT
//     a deliberate non-YOLO declaration — Claude Code often reports a non-empty,
//     non-bypass mode (e.g. "default"/"acceptEdits") for coder subagents inside
//     a YOLO run. So for subagents we ALWAYS fall through to the parent-chain
//     inheritance walk regardless of the subagent's own permission_mode.
//
// When YOLO is inherited from an ancestor, a debug log line is emitted for
// auditability.
func isYoloWithInheritance(event *CloudEvent, wipnoteDir string, database *sql.DB, sessionID, projectDir string, isSubagent bool) bool {
	if isYoloFromEvent(event, wipnoteDir) {
		return true
	}
	// An explicit non-empty, non-bypass permission_mode on a TOP-LEVEL session
	// is a deliberate non-YOLO declaration — do not override with an ancestor's
	// posture. Subagents do NOT get this short-circuit: their reported mode is
	// not a deliberate declaration, so they fall through to the parent walk.
	if !isSubagent && event.PermissionMode != "" {
		return false
	}
	// Inherit from the session family root (subagents always reach here;
	// top-level sessions reach here only when they have no declared mode).
	// database is no longer consulted — ancestry comes from the canonical
	// session-family index, not the vanished sessions table.
	_ = database
	root := yoloFamilyRoot(wipnoteDir, sessionID)
	if root == "" {
		return false // no distinct ancestor
	}
	if isYoloFromRecordedMode(wipnoteDir, root) {
		debugLog(projectDir, "[wipnote] yolo inherited: session=%s parent=%s",
			sessionID, root)
		return true
	}
	return false
}

// anyParentSessionYolo returns true when sessionID's session-family root is in
// YOLO posture. Used as a resilient defense-in-depth signal for the
// worktree-isolation guard so that a yolo-context subagent is still blocked from
// editing main/master even if the primary IsYoloMode detection resolved false.
// Returns false on an empty session or when there is no distinct ancestor.
//
// database is accepted for call-shape compatibility and ignored: ancestry is
// read from the canonical session-family index (see yoloFamilyRoot).
func anyParentSessionYolo(database *sql.DB, wipnoteDir, sessionID string) bool {
	_ = database
	root := yoloFamilyRoot(wipnoteDir, sessionID)
	return root != "" && isYoloFromRecordedMode(wipnoteDir, root)
}

// permissionModeFileName is the per-session marker recording the last
// permission mode the harness reported for that session. It lives beside the
// existing .session-pid / .collector-pid anchors in
// .wipnote/sessions/<session-id>/, which is already the canonical home for
// per-session process facts (session_liveness.go).
//
// This replaces the sessions.metadata JSON column the ConfigChange hook used to
// UPDATE. That column lived only in the per-project SQLite read index; once the
// hook tree moved to a process-local in-memory projection (feat-fc3cc9e0) the
// write went to a throwaway database and every YOLO-posture read came back
// false — silently disarming the inheritance half of the guard. A file also
// satisfies the project's own hook rule: gates answer from durable file state,
// not from state that evaporates with the process.
const permissionModeFileName = ".permission-mode"

// permissionModePath is the marker path for one session. Empty when either
// component is missing, which every caller treats as "no record".
func permissionModePath(wipnoteDir, sessionID string) string {
	if wipnoteDir == "" || sessionID == "" {
		return ""
	}
	return filepath.Join(wipnoteDir, "sessions", sessionID, permissionModeFileName)
}

// RecordSessionPermissionMode durably records the harness-reported permission
// mode for sessionID. Best-effort by contract: the ConfigChange hook must never
// fail a session over a marker write, and a missing marker degrades to
// "no recorded posture" (not YOLO), which is the safe direction — guards stay
// ON rather than being skipped.
func RecordSessionPermissionMode(wipnoteDir, sessionID, mode string) error {
	path := permissionModePath(wipnoteDir, sessionID)
	if path == "" || strings.TrimSpace(mode) == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.TrimSpace(mode)+"\n"), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// recordedSessionPermissionMode reads back the marker, or "" when absent.
func recordedSessionPermissionMode(wipnoteDir, sessionID string) string {
	path := permissionModePath(wipnoteDir, sessionID)
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// isYoloFromRecordedMode reports whether the session's last recorded permission
// mode was bypassPermissions. It is the fallback consulted when the live
// CloudEvent carries no permission_mode of its own.
func isYoloFromRecordedMode(wipnoteDir, sessionID string) bool {
	return recordedSessionPermissionMode(wipnoteDir, sessionID) == "bypassPermissions"
}

// yoloFamilyRoot returns the family-root session for sessionID when that root is
// a DIFFERENT session — i.e. sessionID is a descendant whose posture may be
// inherited. Empty when there is no distinct ancestor to inherit from.
//
// The session family index (.wipnote/session-families.json, written by
// SessionStart) is the canonical ancestry record now that sessions.parent_session_id
// is gone with the read index. For posture inheritance the family ROOT is the
// right ancestor to consult — the launch that established the YOLO posture in
// the first place — rather than one immediate parent hop.
func yoloFamilyRoot(wipnoteDir, sessionID string) string {
	if wipnoteDir == "" || sessionID == "" {
		return ""
	}
	root := agent.SessionFamilyFor(filepath.Dir(wipnoteDir), sessionID)
	if root == "" || root == sessionID {
		return ""
	}
	return root
}
