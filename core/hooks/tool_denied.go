package hooks

import "time"

// Denied tool calls count as failed, never aborted (feat-ddeaa0d1).
//
// A denied call fires PreToolUse, so the tool log holds its start line, but
// Claude Code fires neither PostToolUse nor PostToolUseFailure for it
// (https://code.claude.com/docs/en/hooks#posttoolusefailure: "Permission
// denials fire PreToolUse but not this event"). Two records close that gap:
//
//   - PermissionDenied (this file) fires when auto mode denies a call. Its
//     payload carries tool_use_id, tool_name, tool_input and reason
//     (https://code.claude.com/docs/en/hooks#permissiondenied-input), so the
//     failed line is keyed on the same id PreToolUse used.
//   - Stop and SessionEnd read the transcript tail for the denials the hook
//     never sees: a manual "no" in the permission dialog, a deny rule, an
//     approval left unanswered (tool_failures_transcript.go).

// PermissionDenied handles the Claude Code PermissionDenied hook event. It is
// observe-only: it never asks for a retry.
func PermissionDenied(event *CloudEvent) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	id := toolCallEventID(sessionID, event.ToolUseID)
	recordFailedToolCall(projectDir, sessionID, toolStart{id: id, tool: event.ToolName, at: time.Now().UTC()},
		"permission-denied")
	return &HookResult{Continue: true}, nil
}

// recordFailedToolCall writes the failed completion line for a call and, when
// that lands, a failed entry in the session HTML activity log.
func recordFailedToolCall(projectDir, sessionID string, s toolStart, reason string) bool {
	if projectDir == "" || !appendToolCompletion(projectDir, sessionID, s.id, "failed") {
		return false
	}
	AppendEventToSessionHTML(projectDir, sessionID, SessionEvent{
		Timestamp: s.at,
		ToolName:  s.tool,
		Success:   false,
		EventID:   s.id,
		Summary:   "[failed] " + s.tool + " was not run (" + reason + ")",
		Status:    "failed",
		Reason:    reason,
	})
	return true
}
