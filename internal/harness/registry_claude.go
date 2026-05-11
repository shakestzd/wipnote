package harness

// _claudeOtelID must match otel.HarnessClaude ("claude_code").
// Cross-package assertion is in registry_test.go (TestRegistry_IDsMatchOtelConsts).
const _claudeOtelID = "claude_code"

func init() {
	// Compile-time-ish guard: the condition is always false, but the const
	// reference forces the compiler to keep it, and a future typo edit would
	// be caught at test time by TestRegistry_IDsMatchOtelConsts.
	if _claudeOtelID != "claude_code" {
		panic("harness: _claudeOtelID mismatch — must equal otel.HarnessClaude")
	}
	Register(&HarnessConfig{
		ID:             _claudeOtelID,
		AgentID:        "claude",
		ServiceNames:   []string{"claude-code"},
		SessionAttr:    "session.id",
		HookEventNames: nil,
		HooksHarness:   HooksClaude,
		LaunchEnv:      []string{"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1"},
	})
}
