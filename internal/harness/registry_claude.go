package harness

import "fmt"

// _claudeOtelID must match otel.HarnessClaude ("claude_code").
// Cross-package assertion is in registry_test.go (TestRegistry_IDsMatchOtelConsts).
const _claudeOtelID = "claude_code"

// Env var name constants for the Claude OTel env injection.
// Declared here so the names are co-located with the function that uses them.
const (
	claudeEnvEnableTelemetry    = "CLAUDE_CODE_ENABLE_TELEMETRY"
	claudeEnvEnhancedTelemetry  = "CLAUDE_CODE_ENHANCED_TELEMETRY_BETA"
	claudeEnvMetricsExporter    = "OTEL_METRICS_EXPORTER"
	claudeEnvLogsExporter       = "OTEL_LOGS_EXPORTER"
	claudeEnvTracesExporter     = "OTEL_TRACES_EXPORTER"
	claudeEnvOTLPProtocol       = "OTEL_EXPORTER_OTLP_PROTOCOL"
	claudeEnvOTLPEndpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	claudeEnvLogToolDetails     = "OTEL_LOG_TOOL_DETAILS"
	claudeEnvLogUserPrompts     = "OTEL_LOG_USER_PROMPTS"
	claudeEnvLogToolContent     = "OTEL_LOG_TOOL_CONTENT"
)

// claudeOtelEnv returns the OTel-related environment variables to inject when
// launching Claude Code. The port comes from the per-session collector spawned
// by the launcher. The sessionID argument is ignored by Claude (no session.id
// env var) and is present only to satisfy the OtelEnvFunc signature.
func claudeOtelEnv(port int, sessionID string) []string {
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	return []string{
		claudeEnvEnableTelemetry + "=1",
		claudeEnvEnhancedTelemetry + "=1",
		claudeEnvMetricsExporter + "=otlp",
		claudeEnvLogsExporter + "=otlp",
		claudeEnvTracesExporter + "=otlp",
		claudeEnvOTLPProtocol + "=http/protobuf",
		claudeEnvOTLPEndpoint + "=" + endpoint,
		claudeEnvLogToolDetails + "=1",
		claudeEnvLogUserPrompts + "=1",
		claudeEnvLogToolContent + "=1",
	}
}

func init() {
	// Compile-time-ish guard: the condition is always false, but the const
	// reference forces the compiler to keep it, and a future typo edit would
	// be caught at test time by TestRegistry_IDsMatchOtelConsts.
	if _claudeOtelID != "claude_code" {
		panic("harness: _claudeOtelID mismatch — must equal otel.HarnessClaude")
	}
	cfg := &HarnessConfig{
		ID:             _claudeOtelID,
		AgentID:        "claude",
		ServiceNames:   []string{"claude-code"},
		SessionAttr:    "session.id",
		HookEventNames: nil,
		HooksHarness:   HooksClaude,
		OtelEnv:        claudeOtelEnv,
		LaunchEnv:      []string{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1"},
	}
	if cfg.OtelEnv == nil {
		panic("harness: claude OtelEnv must be non-nil")
	}
	Register(cfg)
}
