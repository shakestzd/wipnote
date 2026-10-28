package hooks

import (
	"path/filepath"
	"time"
)

// Stop handles the Stop hook event (the agent finished a turn). It finalizes
// the session HTML, whose event count is the canonical tool log, for
// harnesses such as Codex that map their task-complete event here rather
// than to SessionEnd, then runs the session-exit reconcile. It writes no
// database rows (feat-174dc958).
func Stop(event *CloudEvent) (*HookResult, error) {
	sessionID := resolveStopSessionID(event)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	recordDelegateHook(event, projectDir, sessionID, "stop")
	// Denied calls in the turn just ended count as failed (feat-ddeaa0d1).
	recordTranscriptFailuresFor(event, projectDir, sessionID)
	// Background tasks that ended since the last turn (feat-36658c22).
	syncTranscriptRunsFor(event, projectDir, sessionID, runStopPass)
	FinalizeSessionHTML(projectDir, sessionID, time.Now().UTC().Format(time.RFC3339), "completed",
		canonicalToolEventCount(projectDir, sessionID))

	// Session-exit reconciliation (slice-5, feat-f93fe770). It blocks
	// (BlockExit2Error) ONLY for harness=="claude" on ambiguous generator
	// drift; Gemini/Codex persist a durable warning instead. skipPortDrift is
	// true because Stop fires on every model response: the port-drift check
	// runs at commit time (commit_portdrift_guard.go).
	if err := runSessionExitReconcile(projectDir, currentHarness().String(), sessionID, true); err != nil {
		return nil, err
	}
	return &HookResult{Continue: true}, nil
}

// resolveStopSessionID is the Stop handler's session-id resolution
// (resolveSessionIDWithHarness with the EnvSessionID fallback). Returns "" when
// no session can be resolved.
func resolveStopSessionID(event *CloudEvent) string {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		sessionID = EnvSessionID(event.SessionID)
	}
	return sessionID
}

// currentHarness resolves the active harness inside a hook handler. Handlers
// do not receive the parsed Harness value, but DetectHarness's env-based
// branches (CLAUDE_CODE_ENTRYPOINT, WIPNOTE_AGENT_ID) are authoritative and
// payload-independent — passing nil short-circuits payload parsing and yields
// the env-derived harness, which is exactly the discriminator slice-5 needs.
func currentHarness() Harness {
	return DetectHarness(nil)
}

// PermissionRequest handles the PermissionRequest Claude Code hook event.
//
// CONTROLLING CONTRACT (AdditiveControlling, feat-b396bd33): CC reads
// hookSpecificOutput.decision.behavior ("allow" | "deny") to decide the
// permission outcome. wipnote uses this CONSERVATIVELY:
//   - It auto-ALLOWS only the tight, read-only allowlist in
//     permission_allowlist.go (and only when risk_level is low/absent).
//   - For EVERYTHING ELSE it emits NO decision, so CC falls through to its
//     normal interactive prompt. wipnote NEVER auto-DENIES.
//
// It records no database row (feat-174dc958); inside a delegate worker a
// prompt that reaches the person is a permission_request event (slice 11).
func PermissionRequest(event *CloudEvent) (*HookResult, error) {
	result := &HookResult{Continue: true}
	applyPermissionAutoAllow(event, result)
	if result.HookSpecificOutput == nil {
		recordDelegatePermission(event) // the person sees this prompt
	}
	return result, nil
}

// PermissionDecisionOnly returns the PermissionRequest decision without
// recording anything. A headless run wipnote started has no one to answer a
// prompt, so it must keep the auto-approval even though it records nothing.
func PermissionDecisionOnly(event *CloudEvent) *HookResult {
	result := &HookResult{Continue: true}
	applyPermissionAutoAllow(event, result)
	return result
}

// applyPermissionAutoAllow is the conservative auto-allow: only a
// demonstrably read-only, allowlisted op at low/absent risk gets a
// behavior:"allow"; all other requests fall through to CC's normal prompt (no
// decision emitted).
func applyPermissionAutoAllow(event *CloudEvent, result *HookResult) {
	if !permissionAutoAllow(event) {
		return
	}
	result.HookSpecificOutput = &HookSpecificOutput{
		HookEventName: "PermissionRequest",
		Decision: &PermissionDecision{
			Behavior: "allow",
			Message:  "Auto-approved: read-only wipnote query (wipnote allowlist).",
		},
	}
}

// ConfigChange handles the ConfigChange Claude Code hook event.
//
// It records the session's permission_mode durably so YOLO detection has a
// posture to fall back on when a later event carries no permission_mode of its
// own (isYoloFromRecordedMode). This used to UPDATE sessions.metadata in the
// per-project read index; that index is gone, so the record now lives in the
// session's own directory under .wipnote/ (yolo_guard.go).
func ConfigChange(event *CloudEvent) (*HookResult, error) {
	if event.PermissionMode == "" {
		return &HookResult{Continue: true}, nil
	}
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	if projectDir == "" {
		return &HookResult{Continue: true}, nil
	}
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	if err := RecordSessionPermissionMode(wipnoteDir, sessionID, event.PermissionMode); err != nil {
		debugLog(projectDir, "[error] handler=config-change session=%s: record permission mode: %v",
			sessionID[:minSessionLen(sessionID)], err)
	}
	return &HookResult{Continue: true}, nil
}

// PostToolUseFailure handles the PostToolUseFailure Claude Code hook event. A
// failed tool fires this instead of PostToolUse, so it writes the "failed"
// completion line that pairs the PreToolUse start in the canonical tool log;
// without it the session-start sweep would record the call as aborted
// (feat-174dc958). It is observe-only.
func PostToolUseFailure(event *CloudEvent) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	appendToolCompletion(projectDir, sessionID, toolCallEventID(sessionID, event.ToolUseID), "failed")
	recordDelegateToolEnd(event, projectDir, sessionID)
	return &HookResult{Continue: true}, nil
}
