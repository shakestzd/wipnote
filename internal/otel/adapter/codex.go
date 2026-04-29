package adapter

import (
	"time"

	"github.com/shakestzd/htmlgraph/internal/otel"
)

// CodexAdapter converts Codex CLI OTel emissions into UnifiedSignals.
// Schema derived from https://github.com/openai/codex/pull/2103.
//
// Identification: resource service.name == "codex-cli".
//
// Session ID attribute: conversation.id (signal-level, with resource fallback).
// PromptID: gen_ai.prompt_id when present; otherwise empty. Codex does not
// emit a stable per-prompt ID on every signal, so callers needing prompt-level
// correlation must group by (SessionID, Timestamp window).
//
// Canonical attribute mapping:
//
//	service.name=codex-cli   conversation.id → SessionID
//	gen_ai.prompt_id         → PromptID (when present)
type CodexAdapter struct{}

// NewCodexAdapter returns a CodexAdapter ready for use.
func NewCodexAdapter() *CodexAdapter {
	return &CodexAdapter{}
}

func (c *CodexAdapter) Name() otel.Harness { return otel.HarnessCodex }

func (c *CodexAdapter) Identify(res OTLPResource) bool {
	return AttrString(res.Attrs, "service.name") == "codex-cli"
}

// ConvertMetric maps Codex metric data points into canonical signals.
// Unknown metric names produce CanonicalUnknown, preserving all attrs in
// RawAttrs for drill-through.
func (c *CodexAdapter) ConvertMetric(res OTLPResource, scope OTLPScope, m OTLPMetric) []otel.UnifiedSignal {
	base := c.baseSignal(res, scope, otel.KindMetric, m.Name, m.Timestamp, m.Attrs)

	switch m.Name {
	case "codex.token.usage", "gen_ai.client.token.usage":
		base.CanonicalName = otel.CanonicalTokenUsage
		base.Model = AttrString(m.Attrs, "gen_ai.response.model")
		if base.Model == "" {
			base.Model = AttrString(m.Attrs, "model")
		}
		switch AttrString(m.Attrs, "gen_ai.token.type") {
		case "input":
			base.Tokens.Input = int64(m.Value)
		case "output":
			base.Tokens.Output = int64(m.Value)
		case "reasoning":
			base.Tokens.Reasoning = int64(m.Value)
		}
	case "codex.session.count":
		base.CanonicalName = otel.CanonicalSessionStart
	case "codex.cost.usage":
		base.CanonicalName = otel.CanonicalTokenUsage
		base.CostUSD = m.Value
		base.CostSource = otel.CostSourceVendor
		base.Model = AttrString(m.Attrs, "model")
	default:
		base.CanonicalName = otel.CanonicalUnknown
	}

	return []otel.UnifiedSignal{base}
}

// ConvertLog maps Codex log events onto canonical names. The event naming
// convention follows OpenAI's gen_ai semconv where applicable.
func (c *CodexAdapter) ConvertLog(res OTLPResource, scope OTLPScope, l OTLPLog) []otel.UnifiedSignal {
	base := c.baseSignal(res, scope, otel.KindLog, l.Name, l.Timestamp, l.Attrs)
	base.TraceID = l.TraceID
	base.SpanID = l.SpanID

	switch l.Name {
	case "codex.user_prompt", "user_prompt":
		base.CanonicalName = otel.CanonicalUserPrompt
	case "codex.api_request", "gen_ai.client.operation.duration":
		base.CanonicalName = otel.CanonicalAPIRequest
		base.Model = AttrString(l.Attrs, "gen_ai.response.model")
		if base.Model == "" {
			base.Model = AttrString(l.Attrs, "model")
		}
		base.DurationMs = AttrInt64(l.Attrs, "duration_ms")
		base.Tokens.Input = AttrInt64(l.Attrs, "gen_ai.usage.input_tokens")
		base.Tokens.Output = AttrInt64(l.Attrs, "gen_ai.usage.output_tokens")
		base.Tokens.Reasoning = AttrInt64(l.Attrs, "gen_ai.usage.reasoning_tokens")
	case "codex.api_error":
		base.CanonicalName = otel.CanonicalAPIError
		base.Model = AttrString(l.Attrs, "model")
		base.ErrorMsg = AttrString(l.Attrs, "error")
		base.Attempt = int(AttrInt64(l.Attrs, "attempt"))
		fval := false
		base.Success = &fval
	case "codex.tool_result", "tool_result":
		base.CanonicalName = otel.CanonicalToolResult
		base.ToolName = AttrString(l.Attrs, "tool_name")
		base.DurationMs = AttrInt64(l.Attrs, "duration_ms")
		base.Decision = AttrString(l.Attrs, "decision")
		base.DecisionSource = normalizeDecisionSource(AttrString(l.Attrs, "source"))
		succ := AttrString(l.Attrs, "success") == "true"
		base.Success = &succ
	case "codex.tool_decision", "tool_decision":
		base.CanonicalName = otel.CanonicalToolDecision
		base.ToolName = AttrString(l.Attrs, "tool_name")
		base.Decision = AttrString(l.Attrs, "decision")
		base.DecisionSource = normalizeDecisionSource(AttrString(l.Attrs, "source"))
	default:
		base.CanonicalName = otel.CanonicalUnknown
	}

	return []otel.UnifiedSignal{base}
}

// ConvertSpan maps Codex span taxonomy onto canonical names.
func (c *CodexAdapter) ConvertSpan(res OTLPResource, scope OTLPScope, s OTLPSpan) []otel.UnifiedSignal {
	base := c.baseSignal(res, scope, otel.KindSpan, s.Name, s.StartTime, s.Attrs)
	base.TraceID = s.TraceID
	base.SpanID = s.SpanID
	base.ParentSpan = s.ParentSpanID
	base.DurationMs = AttrInt64(s.Attrs, "duration_ms")
	if base.DurationMs == 0 && !s.EndTime.IsZero() && !s.StartTime.IsZero() {
		base.DurationMs = s.EndTime.Sub(s.StartTime).Milliseconds()
	}
	if s.StatusCode == 1 {
		v := true
		base.Success = &v
	} else if s.StatusCode == 2 {
		v := false
		base.Success = &v
		base.ErrorMsg = s.StatusMsg
	}

	switch s.Name {
	case "codex.interaction", "ai.interaction":
		base.CanonicalName = otel.CanonicalInteraction
	case "codex.llm_request", "gen_ai.client.operation":
		base.CanonicalName = otel.CanonicalAPIRequest
		base.Model = AttrString(s.Attrs, "gen_ai.response.model")
		if base.Model == "" {
			base.Model = AttrString(s.Attrs, "gen_ai.request.model")
		}
		base.Tokens.Input = AttrInt64(s.Attrs, "gen_ai.usage.input_tokens")
		base.Tokens.Output = AttrInt64(s.Attrs, "gen_ai.usage.output_tokens")
		base.Tokens.Reasoning = AttrInt64(s.Attrs, "gen_ai.usage.reasoning_tokens")
	case "codex.tool", "ai.tool":
		base.CanonicalName = otel.CanonicalToolResult
		base.ToolName = AttrString(s.Attrs, "tool_name")
		if base.ToolName == "" {
			base.ToolName = AttrString(s.Attrs, "gen_ai.tool.name")
		}
	default:
		base.CanonicalName = otel.CanonicalUnknown
	}

	return []otel.UnifiedSignal{base}
}

// baseSignal populates the correlation IDs common to all Codex signals.
// SessionID is sourced from the signal-level conversation.id attribute,
// falling back to the resource-level conversation.id when absent (mirrors
// the Claude adapter's session.id resource fallback at claude.go:246-249).
func (c *CodexAdapter) baseSignal(
	res OTLPResource, scope OTLPScope, kind otel.Kind, name string,
	ts time.Time, attrs map[string]any,
) otel.UnifiedSignal {
	sig := otel.UnifiedSignal{
		Harness:        otel.HarnessCodex,
		HarnessVersion: AttrString(res.Attrs, "service.version"),
		Kind:           kind,
		NativeName:     name,
		Timestamp:      ts,
		SessionID:      AttrString(attrs, "conversation.id"),
		PromptID:       AttrString(attrs, "gen_ai.prompt_id"),
		RawAttrs:       copyAttrs(attrs),
	}
	if sig.SessionID == "" {
		// Cardinality-controlled metrics may omit conversation.id from the
		// data point. Fall back to the resource-level attribute.
		sig.SessionID = AttrString(res.Attrs, "conversation.id")
	}
	return sig
}
