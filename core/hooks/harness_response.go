package hooks

import (
	"encoding/json"
	"io"
	"os"
	"strings"
)

// HookResponse is the normalised internal response that all handlers return.
// It is an alias for HookResult so the rest of the codebase is unchanged; the
// harness-specific emitters read from it.
//
// Fields:
//   - Continue:         non-blocking hooks should set this to true.
//   - Decision:         "allow" | "block" | "deny" (blocking hooks only).
//   - Reason:           human-readable reason (used when Decision != "").
//   - AdditionalContext: Claude's inject-into-conversation field.
//
// Emitters map these fields to the harness-specific wire format.
type HookResponse = HookResult

// claudeContextEvents lists the Claude Code events whose hookSpecificOutput
// accepts additionalContext (verified live with nested `claude -p` and against
// https://code.claude.com/docs/en/hooks, 2026-10). Claude Code IGNORES a
// top-level "additionalContext" key entirely; context only reaches the model
// via hookSpecificOutput.additionalContext + hookEventName.
var claudeContextEvents = map[string]bool{
	"SessionStart": true, "SubagentStart": true, "UserPromptSubmit": true,
	"UserPromptExpansion": true, "PreToolUse": true, "PostToolUse": true,
	"PostToolUseFailure": true, "Stop": true, "SubagentStop": true,
}

// claudeResponse is the Claude Code wire format. Top-level additionalContext
// is deliberately absent (it is silently dropped by Claude Code).
type claudeResponse struct {
	Continue           bool                `json:"continue,omitempty"`
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	Message            string              `json:"message,omitempty"`
	LegacyContext      string              `json:"additionalContext,omitempty"` // only for events with no context channel
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// emitClaudeResponseForEvent writes the Claude Code wire-format JSON to w.
//   - AdditionalContext is mapped into hookSpecificOutput.additionalContext
//     with hookEventName for events that accept it.
//   - PreToolUse blocks use hookSpecificOutput.permissionDecision:"deny" +
//     permissionDecisionReason; other events keep top-level decision/reason
//     (UserPromptSubmit, PostToolUse, Stop, SubagentStop, ... per docs).
//   - Existing HookSpecificOutput (PermissionRequest decision, updatedInput)
//     is preserved. An empty result still encodes as "{}".
func emitClaudeResponseForEvent(w io.Writer, hookEventName string, result *HookResult) error {
	resp := claudeResponse{
		Continue: result.Continue,
		Decision: result.Decision,
		Reason:   result.Reason,
		Message:  result.Message,
	}
	var hso *HookSpecificOutput
	if result.HookSpecificOutput != nil {
		c := *result.HookSpecificOutput
		hso = &c
	}
	event := hookEventName
	if hso != nil && hso.HookEventName != "" {
		event = hso.HookEventName
	}
	if hso != nil {
		hso.AdditionalContext = capContext(hso.AdditionalContext, contextCapForEvent(event))
	}
	if ctx := capContext(result.AdditionalContext, contextCapForEvent(event)); ctx != "" {
		if claudeContextEvents[event] {
			if hso == nil {
				hso = &HookSpecificOutput{}
			}
			hso.AdditionalContext = ctx
		} else {
			resp.LegacyContext = ctx
		}
	}
	if event == "PreToolUse" && (result.Decision == "block" || result.Decision == "deny") {
		if hso == nil {
			hso = &HookSpecificOutput{}
		}
		hso.PermissionDecision = "deny"
		hso.PermissionDecisionReason = result.Reason
		resp.Decision, resp.Reason = "", ""
	}
	if hso != nil {
		if hso.HookEventName == "" {
			hso.HookEventName = event
		}
		resp.HookSpecificOutput = hso
	}
	return json.NewEncoder(w).Encode(resp)
}

// codexHookSpecificOutput is the wire-format shape of Codex's
// hookSpecificOutput object. Its field set is Codex's declared schema —
// verified live against codex-cli 0.147.0 (bug-c6b550fa). Codex's parser
// fails CLOSED on schema mismatch in a way that fails hook enforcement OPEN:
// a single field it does not recognise inside hookSpecificOutput causes
// Codex to silently drop the entire deny (no error, no warning — the tool
// just runs). Because the type is declared at package scope (not inline
// inside emitCodexResponseForEvent), TestCodexHookSpecificOutput_SchemaFieldsPinned
// in harness_test.go can reflect over it directly and fail loudly the moment
// a field is added, renamed, or removed here — before it ever reaches a real
// Codex session and silently disables gating. Do NOT add fields to this
// struct without confirming against Codex's actual schema first; if you need
// to carry wipnote-internal data through this path, do it out-of-band (e.g.
// systemMessage) rather than growing this struct.
type codexHookSpecificOutput struct {
	HookEventName            string `json:"hookEventName,omitempty"`
	AdditionalContext        string `json:"additionalContext,omitempty"`
	PermissionDecision       string `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"`
}

// codexResponse is the top-level wire-format shape of a Codex hook response.
type codexResponse struct {
	Continue           *bool                    `json:"continue,omitempty"`
	StopReason         string                   `json:"stopReason,omitempty"`
	SystemMessage      string                   `json:"systemMessage,omitempty"`
	Decision           string                   `json:"decision,omitempty"`
	Reason             string                   `json:"reason,omitempty"`
	HookSpecificOutput *codexHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// codexInterruptResponse is the whole of Codex's Interrupt output schema.
type codexInterruptResponse struct {
	SystemMessage string `json:"systemMessage,omitempty"`
}

func emitCodexResponseForEvent(w io.Writer, hookEventName string, result *HookResult) error {
	// Interrupt (Codex 0.150.0+, fired when the user cancels a turn) has the
	// narrowest output schema of any event: interrupt.command.output is
	// deny_unknown_fields with systemMessage as its only field
	// (codex-rs/hooks/src/schema.rs InterruptCommandOutputWire). The universal
	// "continue" this emitter otherwise adds would fail the hook.
	if hookEventName == "Interrupt" {
		return json.NewEncoder(w).Encode(codexInterruptResponse{SystemMessage: result.Message})
	}
	continueTrue := true
	resp := codexResponse{}
	if result.Message != "" {
		resp.SystemMessage = result.Message
	}

	hookOutput := codexHookSpecificOutput{HookEventName: hookEventName}
	if result.HookSpecificOutput != nil {
		if result.HookSpecificOutput.HookEventName != "" {
			hookOutput.HookEventName = result.HookSpecificOutput.HookEventName
		}
		hookOutput.AdditionalContext = result.HookSpecificOutput.AdditionalContext
		hookOutput.PermissionDecision = result.HookSpecificOutput.PermissionDecision
		hookOutput.PermissionDecisionReason = result.HookSpecificOutput.PermissionDecisionReason
	}
	if result.AdditionalContext != "" {
		hookOutput.AdditionalContext = result.AdditionalContext
	}
	if hookOutput.HookEventName == "" && hookOutput.AdditionalContext != "" {
		hookOutput.HookEventName = "SessionStart"
	}

	if result.Decision == "block" || result.Decision == "deny" {
		switch {
		case hookEventName == "PreToolUse":
			hookOutput.PermissionDecision = "deny"
			hookOutput.PermissionDecisionReason = result.Reason
		case codexEventAcceptsDecision(hookEventName):
			// Codex's BlockDecisionWire only knows "block"; "deny" fails parsing.
			resp.Decision = "block"
			resp.Reason = result.Reason
		default:
			// SessionStart and the other universal-only events reject a
			// top-level decision/reason, and Codex reports the whole output
			// as "Failed" with no text. continue:false + stopReason is the
			// universal way to stop and still name the cause.
			continueFalse := false
			resp.Continue = &continueFalse
			resp.StopReason = result.Reason
		}
	} else if hookEventName != "PreToolUse" {
		resp.Continue = &continueTrue
	}

	if hookOutput.AdditionalContext != "" || hookOutput.PermissionDecision != "" || hookOutput.PermissionDecisionReason != "" {
		resp.HookSpecificOutput = &hookOutput
	}

	return json.NewEncoder(w).Encode(resp)
}

// codexEventAcceptsDecision reports whether Codex's output schema for
// hookEventName has top-level "decision"/"reason" fields. Codex rejects unknown
// fields, so emitting them for any other event fails the hook. An empty name
// (no event known) keeps the historical decision/reason shape.
// Source: codex-rs/hooks/src/schema.rs *CommandOutputWire structs.
func codexEventAcceptsDecision(hookEventName string) bool {
	switch hookEventName {
	case "", "PostToolUse", "UserPromptSubmit", "Stop", "SubagentStop":
		return true
	default:
		return false
	}
}

func emitGeminiResponseForEvent(w io.Writer, hookEventName string, result *HookResult) error {
	type geminiHookSpecificOutput struct {
		HookEventName     string `json:"hookEventName,omitempty"`
		AdditionalContext string `json:"additionalContext,omitempty"`
	}
	type geminiResponse struct {
		Continue           bool                      `json:"continue"`
		SystemMessage      string                    `json:"systemMessage,omitempty"`
		Decision           string                    `json:"decision,omitempty"`
		Reason             string                    `json:"reason,omitempty"`
		HookSpecificOutput *geminiHookSpecificOutput `json:"hookSpecificOutput,omitempty"`
	}

	resp := geminiResponse{
		Continue: result.Decision != "block" && result.Decision != "deny",
	}
	if result.Message != "" {
		resp.SystemMessage = result.Message
	}
	if result.AdditionalContext != "" {
		resp.HookSpecificOutput = &geminiHookSpecificOutput{
			HookEventName:     hookEventName,
			AdditionalContext: result.AdditionalContext,
		}
	}
	if result.Decision == "block" || result.Decision == "deny" {
		resp.Decision = result.Decision
		resp.Reason = result.Reason
	}

	return json.NewEncoder(w).Encode(resp)
}

// AllowForHarness returns a harness-appropriate "allow" response that can be
// written to stdout via WriteResultForHarness. For Claude, this is an empty
// HookResult{}. For Codex/Gemini, this is a HookResult{Continue: true} which
// will be emitted as their respective wire formats ({"continue": true}).
func AllowForHarness(harness Harness) *HookResult {
	switch harness {
	case HarnessCodex, HarnessGemini, HarnessAntigravity:
		// Codex/Gemini/Antigravity expect {"continue": true} on allow
		return &HookResult{Continue: true}
	default:
		// Claude expects {} (empty object) on allow
		return &HookResult{}
	}
}

// WriteResultForHarness encodes result as JSON to stdout using the wire format
// appropriate for the detected harness. This replaces the harness-agnostic
// WriteResult call in runHookNamed.
func WriteResultForHarness(harness Harness, result *HookResult) error {
	return WriteResultForHarnessEvent(harness, "", result)
}

// WriteResultForHarnessEvent encodes result as JSON with the hook event name
// needed by Codex/Gemini hookSpecificOutput responses.
func WriteResultForHarnessEvent(harness Harness, hookEventName string, result *HookResult) error {
	switch harness {
	case HarnessCodex:
		return emitCodexResponseForEvent(os.Stdout, hookEventName, result)
	case HarnessAntigravity:
		return emitAntigravityResponseForEvent(os.Stdout, hookEventName, result)
	case HarnessGemini:
		return emitGeminiResponseForEvent(os.Stdout, hookEventName, result)
	default:
		return emitClaudeResponseForEvent(os.Stdout, hookEventName, result)
	}
}

// emitAntigravityResponseForEvent writes the Antigravity CLI (agy) hook-result
// wire format. agy decodes a hook's stdout as a protojson hook-result message
// and STRICT-rejects unknown fields — the Gemini-shaped {"continue":true,...}
// response fails with `unknown field "continue"` (verified live, agy v1.0.8),
// so every hook's output was being discarded. A no-op / allow must therefore be
// an empty object "{}".
//
// On the PreInvocation event, wipnote injects model-visible context via the
// result's injectSteps[].systemMessage channel: the orchestrator system prompt
// (from the file named by WIPNOTE_ANTIGRAVITY_SYSTEM_MD, written by the
// launcher) plus any per-prompt additionalContext the handler produced. agy
// merges duplicate system messages across turns, so re-injecting every turn is
// cheap. This is the only working channel to convey the wipnote orchestrator
// directives to agy (GEMINI_SYSTEM_MD / additionalContext / plugin context are
// all ignored by agy).
//
// PreToolUse is gated: agy's PreToolHookResult.allowTool defaults to false, so
// an empty "{}" DENIES every tool (verified live: "Tool call denied by
// jsonhook__wipnote_PreToolUse_0_0"). PreToolUse must therefore emit an explicit
// {"allowTool":true}; a wipnote block emits {"allowTool":false,"denyReason":…}.
func emitAntigravityResponseForEvent(w io.Writer, hookEventName string, result *HookResult) error {
	switch hookEventName {
	case "PreInvocation":
		var sb strings.Builder
		if p := os.Getenv("WIPNOTE_ANTIGRAVITY_SYSTEM_MD"); p != "" {
			if data, err := os.ReadFile(p); err == nil {
				sb.WriteString(strings.TrimSpace(string(data)))
			}
		}
		if result != nil && result.AdditionalContext != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			sb.WriteString(strings.TrimSpace(result.AdditionalContext))
		}
		if sb.Len() > 0 {
			type injectedStep struct {
				SystemMessage string `json:"systemMessage"`
			}
			type preInvocationResult struct {
				InjectSteps []injectedStep `json:"injectSteps"`
			}
			return json.NewEncoder(w).Encode(preInvocationResult{
				InjectSteps: []injectedStep{{SystemMessage: sb.String()}},
			})
		}
		// Nothing to inject — fall through to the empty no-op object.

	case "PreToolUse":
		// agy gates tool execution on PreToolHookResult.allowTool (default
		// false). Must allow explicitly; a wipnote block denies with a reason.
		type preToolResult struct {
			AllowTool  bool   `json:"allowTool"`
			DenyReason string `json:"denyReason,omitempty"`
		}
		if result != nil && (result.Decision == "block" || result.Decision == "deny") {
			return json.NewEncoder(w).Encode(preToolResult{AllowTool: false, DenyReason: result.Reason})
		}
		return json.NewEncoder(w).Encode(preToolResult{AllowTool: true})
	}

	// Allow / no-op for PostToolUse, PostInvocation, Stop, and PreInvocation
	// with nothing to inject. agy's strict protojson decoder accepts "{}".
	_, err := io.WriteString(w, "{}\n")
	return err
}
