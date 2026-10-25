package hooks

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// recordSimpleEvent is a shared helper for hook handlers that record a single
// agent_event and always return Continue. It resolves the session and feature
// IDs from the event/database, builds the AgentEvent, and inserts it
// non-fatally. Returns Continue on missing session ID.
func recordSimpleEvent(
	eventType models.EventType,
	toolName, inputSummary, status string,
	event *CloudEvent,
	database *sql.DB,
) (*HookResult, error) {
	return recordSimpleEventWithOutput(eventType, toolName, inputSummary, "", status, event, database)
}

// recordSimpleEventWithOutput is recordSimpleEvent with an additional
// outputSummary field, used by handlers that want to persist structured detail
// (e.g. PreCompact recording the JSON-encoded context_stats) alongside the
// human-readable input summary. outputSummary may be empty.
func recordSimpleEventWithOutput(
	eventType models.EventType,
	toolName, inputSummary, outputSummary, status string,
	event *CloudEvent,
	database *sql.DB,
) (*HookResult, error) {
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		// For non-Claude harnesses where the session_id was missing,
		// try the env var fallback as last resort.
		sessionID = EnvSessionID(event.SessionID)
	}
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	featureID := cachedGetActiveFeatureID(database, sessionID)
	now := time.Now().UTC()

	ev := &models.AgentEvent{
		EventID:       uuid.New().String(),
		AgentID:       resolveEventAgentID(event),
		EventType:     eventType,
		Timestamp:     now,
		ToolName:      toolName,
		InputSummary:  inputSummary,
		OutputSummary: outputSummary,
		SessionID:     sessionID,
		FeatureID:     featureID,
		Status:        status,
		Source:        "hook",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := db.InsertEvent(database, ev); err != nil {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		debugLog(projectDir, "[error] handler=%s session=%s: insert event: %v", toolName, sessionID[:minSessionLen(sessionID)], err)
	}

	return &HookResult{Continue: true}, nil
}

// recordSimpleEventRouted is recordSimpleEvent for the hot Stop hook: it builds
// the same single agent_event row but routes the INSERT through the daemon-first
// enqueue path (plan-2390966a slice-4) instead of the direct db.InsertEvent.
// Only the Stop handler uses it — the shared recordSimpleEvent stays on the
// direct path for the many cold observational hooks (PreCompact, WorktreeRemove,
// …) that are out of this slice's scope. handler labels the fallback metrics;
// projectRoot (parent of .wipnote/) feeds RouteHookWrite's daemon + DBPath
// resolution. Best-effort: it never blocks and always returns Continue.
func recordSimpleEventRouted(
	handler string,
	eventType models.EventType,
	toolName, inputSummary, status string,
	projectRoot, sessionID string,
	event *CloudEvent,
	database *sql.DB,
) (*HookResult, error) {
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:      uuid.New().String(),
		AgentID:      resolveEventAgentID(event),
		EventType:    eventType,
		Timestamp:    now,
		ToolName:     toolName,
		InputSummary: inputSummary,
		SessionID:    sessionID,
		FeatureID:    cachedGetActiveFeatureID(database, sessionID),
		Status:       status,
		Source:       "hook",
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_ = RouteInsertEvent(handler, projectRoot, sessionID, ev, database)
	return &HookResult{Continue: true}, nil
}

// Stop handles the Stop Claude Code hook event (agent/session stopped).
// Records a checkpoint event and captures the last assistant message as output.
// Also reads the transcript JSONL to persist the assistant reply text as an
// assistant_text otel_signals row so the dashboard can render text-only turns.
func Stop(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	summary := "Agent stopped"
	if event.LastAssistantMessage != "" {
		msg := event.LastAssistantMessage
		if len(msg) > debugMsgMaxLen {
			msg = msg[:debugMsgMaxLen] + "…"
		}
		summary = fmt.Sprintf("Agent stopped: %s", msg)
	}

	// Read the transcript and persist the last assistant text turn as an
	// otel_signals row with canonical='assistant_text'. This is a fast
	// synchronous file read — no network calls. Errors are logged to
	// debug.log only and never block the Stop response.
	sessionID := resolveSessionIDWithHarness(event)
	if sessionID == "" {
		sessionID = EnvSessionID(event.SessionID)
	}
	if sessionID != "" {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		if !insertAssistantTextSignalFromHookPayload(database, projectDir, sessionID, event, event.LastAssistantMessage, "hook_payload") {
			insertAssistantTextSignal(database, projectDir, sessionID, event.TranscriptPath)
		}
		// Backfill any user prompts missed by the live UserPromptSubmit hook path.
		// Non-fatal: errors are logged to debug.log and never block the Stop response.
		if n, err := backfillMissedUserPrompts(database, projectDir, sessionID, event.TranscriptPath); err != nil {
			debugLog(projectDir, "[user-prompt-backfill] stop hook: %v", err)
		} else if n > 0 {
			debugLog(projectDir, "[user-prompt-backfill] stop: %d prompts recovered (session=%s)", n, sessionID[:minSessionLen(sessionID)])
		}
		// Finalize session HTML so the event-count badge is accurate for harnesses
		// (e.g. Codex) that map their task-complete event to this Stop handler
		// rather than to SessionEnd. The count is the session's canonical tool
		// log (feat-af8d1689), not rows in this hook's database handle, which only
		// ever held what earlier hook processes happened to write into it.
		FinalizeSessionHTML(projectDir, sessionID, time.Now().UTC().Format(time.RFC3339), "completed",
			canonicalToolEventCount(projectDir, sessionID))
	}

	// Session-exit reconciliation (slice-5, feat-f93fe770).
	//
	// AMENDMENT to the historical no-block contract: this handler previously
	// NEVER blocked (the only exit-2 wiring was TaskCompleted via
	// task_completion_gate.go). It now blocks (BlockExit2Error) — but ONLY for
	// harness=="claude" AND ONLY on ambiguous generator-drift. This narrow,
	// intentional amendment is scoped here: Gemini/Codex never block (a durable
	// warning is persisted and surfaced at the next SessionStart instead).
	//
	// skipPortDrift=true: Stop fires on EVERY model response (per-turn), not
	// just at true session boundaries. Running the port-drift check here cost
	// 8-22s/turn across 3 full regenerations. Port-drift enforcement is now
	// handled at commit-time by checkPortDriftCommitGuard (commit_portdrift_guard.go),
	// which fires only when generator-input files are staged — so correctness is
	// preserved at zero per-turn cost. The cheap orphan/uncommitted reconcile
	// classes still run on every Stop.
	if sessionID != "" {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		if err := runSessionExitReconcile(database, projectDir,
			currentHarness().String(), sessionID, true); err != nil {
			return nil, err
		}
	}

	// Route the terminal Stop agent_events INSERT through the daemon-first
	// enqueue path (plan-2390966a slice-4). Stop fires on EVERY model response,
	// so this is a hot write: routing it keeps the handler under 1s even when an
	// external writer holds the lock. Best-effort — canonical NDJSON + reindex
	// recover the row if the write degrades.
	return recordSimpleEventRouted("stop", models.EventEnd, "Stop", summary, "recorded",
		ResolveProjectDir(event.CWD, event.SessionID), resolveStopSessionID(event), event, database)
}

// resolveStopSessionID mirrors the Stop handler's session-id resolution
// (resolveSessionIDWithHarness with the EnvSessionID fallback) so the routed
// terminal-event recorder labels and routes under the same session ID the rest
// of the Stop handler used. Returns "" when no session can be resolved (the
// routed recorder then no-ops with Continue, matching recordSimpleEvent).
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
// The checkpoint event is always recorded first (observability is unchanged);
// the auto-allow decision is layered on top of that recorded result.
func PermissionRequest(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	summary := "Permission requested"
	if event.ToolName != "" {
		summary = fmt.Sprintf("Permission requested for tool: %s", event.ToolName)
	}

	result, err := recordSimpleEvent(models.EventCheckPoint, "PermissionRequest", summary, "recorded", event, database)
	if err != nil || result == nil {
		return result, err
	}
	applyPermissionAutoAllow(event, result)
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
//
// database is accepted for hook call-shape compatibility and unused.
func ConfigChange(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	_ = database
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

// PostToolUseFailure handles the PostToolUseFailure Claude Code hook event.
// Records a tool crash/exception as an error event with details from ToolResult.
// This handler is kept separate because it extracts error info from ToolResult.
//
// CONTROLLING CAPABILITY (AdditiveControlling, per hookEventContractSpecs): CC
// reads a JSON HookResult here, so this handler COULD return a controlling
// shape (e.g. {decision:"block"} to halt after a tool failure). It is kept
// observe-only by design — no current use-case to alter CC's failure handling
// — but the HookResult return path is already in place for a future addition.
func PostToolUseFailure(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	featureID := cachedGetActiveFeatureID(database, sessionID)
	errorSummary := summariseOutput(event.ToolResult)
	if errorSummary == "" {
		errorSummary = fmt.Sprintf("tool %q crashed or threw exception", event.ToolName)
	}

	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:      uuid.New().String(),
		AgentID:      resolveEventAgentID(event),
		EventType:    models.EventError,
		Timestamp:    now,
		ToolName:     event.ToolName,
		InputSummary: fmt.Sprintf("PostToolUseFailure: %s", errorSummary),
		SessionID:    sessionID,
		FeatureID:    featureID,
		Status:       "failed",
		Source:       "hook",
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := db.InsertEvent(database, ev); err != nil {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		debugLog(projectDir, "[error] handler=posttooluse-failure session=%s: insert event: %v", sessionID[:minSessionLen(sessionID)], err)
	}

	return &HookResult{Continue: true}, nil
}
