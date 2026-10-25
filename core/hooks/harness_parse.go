package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/harness"
)

// codexToolResult normalises a Codex PostToolUse tool-result payload into the
// map[string]any shape the shared summariser/isSuccess helpers (posttooluse.go,
// missing_events.go) expect — the same shape Claude Code's own tool_result
// object uses.
//
// Live capture (codex-cli 0.147.0, 2026-08-07, bug-9f49a375) showed Codex
// sends this value as a bare JSON string (the raw tool output text, e.g.
// "line1\nline2\n" for Bash or "Plan updated" for update_plan) rather than an
// object — across Bash, update_plan, and apply_patch tool calls, every
// observed payload was a plain string, never an object. UnmarshalJSON accepts
// either shape so a future Codex version that switches to an object (matching
// Claude's convention) keeps working without another silent-discard bug: a
// bare string is wrapped as {"output": <string>} so summariseOutput /
// summariseToolOutput (which look for "output"/"content"/"result" keys) pick
// it up unchanged.
type codexToolResult map[string]any

func (r *codexToolResult) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*r = nil
		return nil
	}
	var obj map[string]any
	if err := json.Unmarshal(data, &obj); err == nil {
		*r = obj
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("codexToolResult: value is neither an object nor a string: %w", err)
	}
	if s == "" {
		*r = nil
		return nil
	}
	*r = map[string]any{"output": s}
	return nil
}

// codexPayload is used only for harness detection and input parsing.
// It matches the flat top-level shape of a Codex CLI hook payload.
//
// ToolResponse/ToolResult: live capture (codex-cli 0.147.0, 2026-08-07,
// bug-9f49a375) confirmed the real PostToolUse field is "tool_response", not
// "tool_result" — the latter was never observed in any live payload (Bash,
// update_plan, apply_patch all used "tool_response"). Both tags are kept and
// merged in parseCodexEvent so an older/different Codex build that happens to
// emit "tool_result" is still read rather than swapping one hardcoded
// assumption for another.
//
// REMOVED fields (bug-e95cfc49):
//   - TaskID and TaskSubject were removed because they belong to Codex Cloud's
//     RawRecord task-tracking schema (alongside encrypted_task_id,
//     max_concurrent_threads_per_session, job_max_runtime_seconds), not to the
//     local hook dispatch vocabulary. Live payload captures (SessionStart,
//     PreToolUse, PostToolUse) never populated them across codex-cli 0.147.0,
//     and Codex-native TaskStarted/TaskComplete lifecycle events (distinct from
//     Claude-style task boards) do not exist in hook payloads. Binary analysis of
//     codex-cli 0.147.0 confirmed task_subject appears zero times in the binary.
//     TaskCreated/TaskCompleted are Claude-only hooks with no Codex equivalent.
type codexPayload struct {
	// AgentID/AgentType: declared as optional string properties on
	// PreToolUse, PostToolUse, and PermissionRequest, and as REQUIRED
	// properties on SubagentStart/SubagentStop, per Codex's own generated
	// JSON Schema (codex-rs/hooks/schema/generated/*.command.input.schema.json,
	// openai/codex @ main, verified 2026-08-08 — the schema is generated from
	// the Rust wire types and is ground truth; the prose docs incorrectly
	// list these fields as subagent-lifecycle-only). Absence on
	// PreToolUse/PostToolUse/PermissionRequest is itself meaningful: Codex's
	// Rust source models the field as `subagent: Option<SubagentHookContext>`
	// (codex-rs/hooks/src/events/pre_tool_use.rs), and codex-rs/hooks/src/
	// schema.rs has a dedicated unit test asserting the root/orchestrator
	// case serializes with NO agent_id/agent_type keys at all — the same
	// "present only inside a subagent call" contract Claude Code documents.
	AgentID              string          `json:"agent_id"`
	AgentType            string          `json:"agent_type"`
	SessionID            string          `json:"session_id"`
	TurnID               string          `json:"turn_id"`
	TranscriptPath       string          `json:"transcript_path"`
	CWD                  string          `json:"cwd"`
	HookEventName        string          `json:"hook_event_name"`
	Model                string          `json:"model"`
	PermissionMode       string          `json:"permission_mode"`
	Timestamp            string          `json:"timestamp"`
	Source               string          `json:"source"`
	Prompt               string          `json:"prompt"`
	LastAssistantMessage string          `json:"last_assistant_message"`
	StopReason           string          `json:"stop_reason"`
	ToolName             string          `json:"tool_name"`
	ToolInput            map[string]any  `json:"tool_input"`
	ToolUseID            string          `json:"tool_use_id"`
	DurationMS           durationMS      `json:"duration_ms"`
	ToolResponse         codexToolResult `json:"tool_response"`
	ToolResult           codexToolResult `json:"tool_result"`
	TaskData             map[string]any  `json:"task"`
}

// geminiPayload is used only for harness detection and input parsing.
// It matches the base input schema of a Gemini CLI hook payload.
// Gemini's unique top-level field is "invocation_id"; it also has
// a nested "tool" object (for BeforeTool/AfterTool events) instead
// of top-level tool_name.
type geminiPayload struct {
	InvocationID   string `json:"invocation_id"`
	SessionID      string `json:"session_id"`
	CWD            string `json:"cwd"`
	Model          string `json:"model"`
	TranscriptPath string `json:"transcript_path"`
	HookEventName  string `json:"hook_event_name"`
	Timestamp      string `json:"timestamp"`
	// BeforeAgent / AfterAgent prompt text field.
	Prompt         string `json:"prompt"`
	PromptResponse string `json:"prompt_response"`
	// BeforeTool / AfterTool nested tool object.
	Tool struct {
		Name  string         `json:"name"`
		Input map[string]any `json:"input"`
	} `json:"tool"`
	// AfterModel LLM request/response fields (available when hook_event_name == "AfterModel").
	LLMRequest  map[string]any `json:"llm_request,omitempty"`
	LLMResponse map[string]any `json:"llm_response,omitempty"`
}

// parseCodexEvent converts a Codex CLI hook payload into our internal CloudEvent
// representation. Codex uses a flat JSON structure with top-level fields like
// "hook_event_name", "cwd", and "session_id". We map those into the CloudEvent
// fields that downstream handlers read.
//
// Hardening: if WIPNOTE_PARENT_AGENT is set to a value other than "codex"
// (e.g. "claude-code"), we use that as AgentID rather than hard-coding "codex".
// This prevents misclassification when a stale env or mis-routed payload reaches
// this parser for a non-Codex harness.
func parseCodexEvent(raw []byte) (*CloudEvent, error) {
	var p codexPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parseCodexEvent: %w", err)
	}

	agentID := harness.GetByHooksHarness(harness.HooksCodex).AgentID
	if parent := strings.TrimSpace(os.Getenv("WIPNOTE_PARENT_AGENT")); parent != "" && parent != agentID {
		agentID = parent
	}
	// bug-190950e0 / feat-b7bc4267: when the payload carries a real
	// per-subagent agent_id (see codexPayload doc comment), it is ground
	// truth from the harness and must win over both the generic harness
	// default and the WIPNOTE_PARENT_AGENT hardening override above.
	// Previously this value was silently discarded here and every Codex
	// event — subagent or not — was stamped with the same generic "codex"
	// identity, collapsing every subagent claim onto __root__ downstream.
	if p.AgentID != "" {
		agentID = p.AgentID
	}

	// Prefer the confirmed-live "tool_response" field; fall back to
	// "tool_result" for any build that uses the other spelling rather than
	// hardcoding a single field name (see codexPayload doc comment).
	toolResult := map[string]any(p.ToolResponse)
	if toolResult == nil {
		toolResult = map[string]any(p.ToolResult)
	}

	ev := &CloudEvent{
		AgentID:              agentID,
		AgentType:            p.AgentType,
		SessionID:            p.SessionID,
		CWD:                  p.CWD,
		HookEventName:        p.HookEventName,
		PermissionMode:       p.PermissionMode,
		Timestamp:            p.Timestamp,
		Model:                p.Model,
		TranscriptPath:       p.TranscriptPath,
		Source:               p.Source,
		Prompt:               p.Prompt,
		TurnID:               p.TurnID,
		LastAssistantMessage: p.LastAssistantMessage,
		StopReason:           p.StopReason,
		ToolName:             p.ToolName,
		ToolInput:            p.ToolInput,
		ToolUseID:            p.ToolUseID,
		DurationMS:           p.DurationMS,
		ToolResult:           toolResult,
		TaskData:             p.TaskData,
	}
	return ev, nil
}

// parseGeminiEvent converts a Gemini CLI hook payload into our internal
// CloudEvent representation. Gemini uses a base input schema with an
// "invocation_id" field. Tool information is nested under a "tool" object for
// BeforeTool/AfterTool events. This parser is best-effort until a real captured
// payload is available for full verification.
func parseGeminiEvent(raw []byte) (*CloudEvent, error) {
	var p geminiPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parseGeminiEvent: %w", err)
	}

	ev := &CloudEvent{
		AgentID: harness.GetByHooksHarness(harness.HooksGemini).AgentID,
		// Gemini may use "invocation_id" as the session identifier;
		// fall back to session_id if present.
		SessionID:      p.SessionID,
		CWD:            p.CWD,
		HookEventName:  p.HookEventName,
		Model:          p.Model,
		TranscriptPath: p.TranscriptPath,
		Timestamp:      p.Timestamp,
		Prompt:         p.Prompt,
		PromptResponse: p.PromptResponse,
		// BeforeTool / AfterTool: tool name is nested under "tool".
		ToolName:  p.Tool.Name,
		ToolInput: p.Tool.Input,
		// AfterModel: LLM request/response payloads.
		LLMRequest:  p.LLMRequest,
		LLMResponse: p.LLMResponse,
	}
	// If session_id is empty, use invocation_id as a surrogate so that
	// session-scoped DB lookups have something to work with.
	if ev.SessionID == "" && p.InvocationID != "" {
		ev.SessionID = p.InvocationID
	}
	return ev, nil
}

// parseAntigravityEvent converts an Antigravity CLI hook payload into our internal
// CloudEvent representation. Antigravity uses the same base input schema as Gemini.
func parseAntigravityEvent(raw []byte) (*CloudEvent, error) {
	var p geminiPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, fmt.Errorf("parseAntigravityEvent: %w", err)
	}

	ev := &CloudEvent{
		AgentID:        harness.GetByHooksHarness(harness.HooksAntigravity).AgentID,
		SessionID:      p.SessionID,
		CWD:            p.CWD,
		HookEventName:  p.HookEventName,
		Model:          p.Model,
		TranscriptPath: p.TranscriptPath,
		Timestamp:      p.Timestamp,
		Prompt:         p.Prompt,
		PromptResponse: p.PromptResponse,
		// BeforeTool / AfterTool: tool name is nested under "tool".
		ToolName:  p.Tool.Name,
		ToolInput: p.Tool.Input,
		// AfterModel: LLM request/response payloads.
		LLMRequest:  p.LLMRequest,
		LLMResponse: p.LLMResponse,
	}
	if ev.SessionID == "" && p.InvocationID != "" {
		ev.SessionID = p.InvocationID
	}
	return ev, nil
}

// ParseEventForHarness reads the raw payload bytes and returns a CloudEvent
// parsed according to the given harness's input schema. For Claude, the
// existing JSON unmarshal path is used (CloudEvent struct tags handle it
// directly). For Codex and Gemini, dialect-specific parsers normalise the
// flat/nested payloads into CloudEvent.
func ParseEventForHarness(harness Harness, raw []byte) (*CloudEvent, error) {
	ev, err := parseEventForHarness(harness, raw)
	if err != nil || ev == nil {
		return ev, err
	}
	// Stamp the resolved harness onto the event so every handler downstream
	// of parsing can consult event.Harness directly instead of redetecting
	// it or requiring a Harness parameter on every hook function signature.
	ev.Harness = harness
	return ev, nil
}

func parseEventForHarness(harness Harness, raw []byte) (*CloudEvent, error) {
	switch harness {
	case HarnessCodex:
		return parseCodexEvent(raw)
	case HarnessGemini:
		return parseGeminiEvent(raw)
	case HarnessAntigravity:
		return parseAntigravityEvent(raw)
	default:
		// Claude: standard CloudEvent unmarshal (existing behaviour).
		if len(raw) == 0 {
			return &CloudEvent{}, nil
		}
		var ev CloudEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return nil, fmt.Errorf("parsing CloudEvent: %w", err)
		}
		normalizeClaudeToolResult(&ev)
		return &ev, nil
	}
}

// ParseClaudeWorktreeCreateEvent parses Claude Code's WorktreeCreate payload.
// WorktreeCreate is a Claude-only replacement hook, and observed Claude
// payloads may wrap event-specific fields inside a top-level "data" object.
// Keep this parser separate from generic harness detection so manual repros
// with hook_event_name are not misclassified as Codex payloads.
func ParseClaudeWorktreeCreateEvent(raw []byte) (*CloudEvent, error) {
	ev, err := ParseEventForHarness(HarnessClaude, raw)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return ev, nil
	}

	var envelope struct {
		Data *CloudEvent `json:"data"`
		// Claude Code (observed 2026-06-11) sends the worktree name as a bare
		// top-level "name" field with no worktree_name/worktree_base_path.
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("parsing WorktreeCreate event: %w", err)
	}
	if envelope.Data != nil {
		overlayMissingCloudEventFields(ev, envelope.Data)
	}
	if ev.WorktreeName == "" {
		ev.WorktreeName = envelope.Name
	}
	return ev, nil
}

func overlayMissingCloudEventFields(dst, src *CloudEvent) {
	if dst == nil || src == nil {
		return
	}
	if dst.SessionID == "" {
		dst.SessionID = src.SessionID
	}
	if dst.CWD == "" {
		dst.CWD = src.CWD
	}
	if dst.TranscriptPath == "" {
		dst.TranscriptPath = src.TranscriptPath
	}
	if dst.WorktreeName == "" {
		dst.WorktreeName = src.WorktreeName
	}
	if dst.WorktreeBasePath == "" {
		dst.WorktreeBasePath = src.WorktreeBasePath
	}
	if dst.WorktreePath == "" {
		dst.WorktreePath = src.WorktreePath
	}
}
