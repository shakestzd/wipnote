package adapter_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/harness"
	"github.com/shakestzd/wipnote/observe/otel"
	"github.com/shakestzd/wipnote/observe/otel/adapter"
)

type genAIConverter interface {
	Name() otel.Harness
	Identify(adapter.OTLPResource) bool
	ConvertMetric(adapter.OTLPResource, adapter.OTLPScope, adapter.OTLPMetric) []otel.UnifiedSignal
	ConvertLog(adapter.OTLPResource, adapter.OTLPScope, adapter.OTLPLog) []otel.UnifiedSignal
	ConvertSpan(adapter.OTLPResource, adapter.OTLPScope, adapter.OTLPSpan) []otel.UnifiedSignal
}

// TestGenAIAdapters_SameMapping pins that the Gemini and Antigravity
// adapters share one mapping: the same OTLP input yields the same canonical
// signals, differing only in the Harness stamp, and each identifies only
// its own registry service names.
func TestGenAIAdapters_SameMapping(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	res := adapter.OTLPResource{Attrs: map[string]any{"service.version": "1.2.3", "session.id": "res-sess"}}
	scope := adapter.OTLPScope{Name: "s"}
	attrs := func(extra map[string]any) map[string]any {
		m := map[string]any{"session.id": "sess-1", "gen_ai.prompt_id": "p-1"}
		for k, v := range extra {
			m[k] = v
		}
		return m
	}
	metrics := []adapter.OTLPMetric{
		{Name: "gemini_cli.token.usage", Value: 12, Timestamp: ts, Attrs: attrs(map[string]any{"type": "input", "model": "m"})},
		{Name: "gen_ai.client.token.usage", Value: 3, Timestamp: ts, Attrs: attrs(map[string]any{"type": "thought"})},
		{Name: "gemini_cli.session.count", Value: 1, Timestamp: ts, Attrs: attrs(nil)},
		{Name: "gemini_cli.tool.decision", Timestamp: ts, Attrs: attrs(nil)},
		{Name: "something.else", Timestamp: ts, Attrs: map[string]any{}},
	}
	logs := []adapter.OTLPLog{
		{Name: "gemini_cli.user_prompt", Timestamp: ts, Attrs: attrs(map[string]any{"prompt_length": int64(4)})},
		{Name: "gemini_cli.api_request", Timestamp: ts, Attrs: attrs(map[string]any{"model": "m", "duration_ms": int64(9)})},
		{Name: "gemini_cli.api_error", Timestamp: ts, Attrs: attrs(map[string]any{"error": "boom"})},
		{Name: "tool_result", Timestamp: ts, Attrs: attrs(map[string]any{"function_name": "read", "success": true})},
		{Name: "tool_decision", Timestamp: ts, Attrs: attrs(nil)},
		{Name: "unknown.log", Timestamp: ts, Attrs: attrs(nil)},
	}
	spans := []adapter.OTLPSpan{
		{Name: "gemini_cli.interaction", TraceID: "t", SpanID: "s1", StartTime: ts, EndTime: ts.Add(time.Second), Attrs: attrs(nil)},
		{Name: "gen_ai.client.operation", TraceID: "t", SpanID: "s2", ParentSpanID: "s1", StartTime: ts, Attrs: attrs(map[string]any{"gen_ai.request.model": "m"})},
		{Name: "ai.tool", SpanID: "s3", StartTime: ts, Attrs: attrs(nil)},
		{Name: "other.span", SpanID: "s4", StartTime: ts, Attrs: map[string]any{}},
	}

	gem, agy := genAIConverter(adapter.NewGeminiAdapter()), genAIConverter(adapter.NewAntigravityAdapter())
	same := func(kind string, g, a []otel.UnifiedSignal) {
		t.Helper()
		if len(g) != len(a) {
			t.Fatalf("%s: %d vs %d signals", kind, len(g), len(a))
		}
		for i := range g {
			if g[i].Harness != otel.HarnessGemini || a[i].Harness != otel.HarnessAntigravity {
				t.Errorf("%s[%d]: harness %q / %q", kind, i, g[i].Harness, a[i].Harness)
			}
			g[i].Harness, a[i].Harness = "", ""
			if !reflect.DeepEqual(g[i], a[i]) {
				t.Errorf("%s[%d] differs:\n gemini      %+v\n antigravity %+v", kind, i, g[i], a[i])
			}
		}
	}
	for _, m := range metrics {
		same("metric "+m.Name, gem.ConvertMetric(res, scope, m), agy.ConvertMetric(res, scope, m))
	}
	for _, l := range logs {
		same("log "+l.Name, gem.ConvertLog(res, scope, l), agy.ConvertLog(res, scope, l))
	}
	for _, s := range spans {
		same("span "+s.Name, gem.ConvertSpan(res, scope, s), agy.ConvertSpan(res, scope, s))
	}

	if gem.Name() != otel.HarnessGemini || agy.Name() != otel.HarnessAntigravity {
		t.Errorf("names = %q, %q", gem.Name(), agy.Name())
	}
	for _, c := range []struct {
		own, other genAIConverter
		id         otel.Harness
	}{{gem, agy, otel.HarnessGemini}, {agy, gem, otel.HarnessAntigravity}} {
		cfg := harness.Get(string(c.id))
		if cfg == nil || len(cfg.ServiceNames) == 0 {
			t.Fatalf("no registry service names for %s", c.id)
		}
		for _, svc := range cfg.ServiceNames {
			r := adapter.OTLPResource{Attrs: map[string]any{"service.name": svc}}
			if !c.own.Identify(r) || c.other.Identify(r) {
				t.Errorf("service %q: own=%v other=%v, want true/false", svc, c.own.Identify(r), c.other.Identify(r))
			}
		}
	}
}
