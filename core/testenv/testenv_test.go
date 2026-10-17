package testenv

import (
	"os"
	"testing"
)

func TestClearAmbientSession(t *testing.T) {
	set := map[string]string{
		"WIPNOTE_AGENT_ID":       "wipnote-feature-coder",
		"WIPNOTE_SESSION_ID":     "sess-leak",
		"WIPNOTE_SOMETHING_NEW":  "x", // prefix match covers future variables
		"CLAUDE_PROJECT_DIR":     "/real/repo",
		"CLAUDE_CODE_SESSION_ID": "0000",
		"CLAUDECODE":             "1",
	}
	for k, v := range set {
		t.Setenv(k, v)
	}
	t.Setenv("CLAUDE_PLUGIN_ROOT", "/plugin") // harness config, not session state

	ClearAmbientSession()

	for k := range set {
		if v, ok := os.LookupEnv(k); ok {
			t.Errorf("%s = %q survived ClearAmbientSession", k, v)
		}
	}
	if got := os.Getenv("CLAUDE_PLUGIN_ROOT"); got != "/plugin" {
		t.Errorf("CLAUDE_PLUGIN_ROOT = %q, want it left alone", got)
	}
}
