package hooks

import (
	"testing"
)

// clearNestedEnv unsets nested-session env that would change the resolved
// session ID or mark the event a subagent (see MEMORY: nested-session-hooks).
func clearNestedEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "WIPNOTE_PARENT_SESSION",
		"WIPNOTE_NESTING_DEPTH", "WIPNOTE_SESSION_ID", "CLAUDE_ENV_FILE",
		"WIPNOTE_SESSION_FAMILY_ID", "WIPNOTE_AGENT_ID",
	} {
		t.Setenv(k, "")
	}
}
