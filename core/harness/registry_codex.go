package harness

// _codexOtelID must match otel.HarnessCodex ("codex").
// Cross-package assertion is in registry_test.go (TestRegistry_IDsMatchOtelConsts).
const _codexOtelID = "codex"

// codexEnvOtelSession carries the wipnote collector session ID to wipnote's
// own hook subprocesses; it is not an OTel SDK variable.
const codexEnvOtelSession = "WIPNOTE_OTEL_SESSION"

// codexOtelEnv returns the environment variables to inject when launching the
// Codex CLI. The sessionID comes from the per-session collector spawned by the
// launcher; port is unused (see below) and kept only for the OtelEnvFunc
// signature.
//
// Deliberately NO OTEL_EXPORTER_OTLP_ENDPOINT / OTEL_SERVICE_NAME: Codex is
// configured only through its [otel] config, which the launcher passes as
// `-c otel.*` (buildCodexOtelConfigArgs). Codex sets the exporter endpoint
// explicitly from that config (codex-rs/otel/src/provider.rs:475 with_endpoint)
// and the service name explicitly in code (provider.rs:364-365
// with_service_name), and nothing in codex-rs reads either env var. Setting
// them did nothing for Codex but leaked into every tool subprocess Codex runs,
// where any OTel-instrumented program would export to wipnote's collector.
func codexOtelEnv(_ int, sessionID string) []string {
	return []string{
		codexEnvOtelSession + "=" + sessionID,
	}
}

func init() {
	if _codexOtelID != "codex" {
		panic("harness: _codexOtelID mismatch — must equal otel.HarnessCodex")
	}
	cfg := &HarnessConfig{
		ID:      _codexOtelID,
		AgentID: "codex",
		// Codex emits two service.name variants: the TypeScript CLI and the Rust rewrite.
		ServiceNames:   []string{"codex-cli", "codex_cli_rs"},
		SessionAttr:    "conversation.id",
		HookEventNames: nil,
		HooksHarness:   HooksCodex,
		OtelEnv:        codexOtelEnv,
	}
	if cfg.OtelEnv == nil {
		panic("harness: codex OtelEnv must be non-nil")
	}
	Register(cfg)
}
