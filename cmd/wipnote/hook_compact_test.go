package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// Outside a wipnote project the hook must stay quiet and allow: an empty
// stdout on Claude (plain stdout would become summary instructions) and a JSON
// allow elsewhere (Codex ignores plain text on PreCompact).
func TestRunPreCompact_NoProjectAllows(t *testing.T) {
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv(headlessHookEnv, "")
	// Claude Code sets this on every hook run; it is what tells a Claude
	// payload (which also carries hook_event_name) from a Codex one.
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("WIPNOTE_AGENT_ID", "")
	cwd := t.TempDir()

	payload := `{"session_id":"abc123","cwd":"` + cwd + `","hook_event_name":"PreCompact","trigger":"auto","custom_instructions":null}`
	claude := captureStdout(t, func() {
		if err := runPreCompact([]byte(payload)); err != nil {
			t.Errorf("claude: %v", err)
		}
	})
	if claude != "" {
		t.Errorf("claude: want empty stdout, got %q", claude)
	}

	old := hookHarnessOverride
	hookHarnessOverride = "codex"
	t.Cleanup(func() { hookHarnessOverride = old })
	payload = `{"session_id":"abc123","cwd":"` + cwd + `","hook_event_name":"PreCompact","turn_id":"t1","trigger":"manual"}`
	codex := captureStdout(t, func() {
		if err := runPreCompact([]byte(payload)); err != nil {
			t.Errorf("codex: %v", err)
		}
	})
	if strings.TrimSpace(codex) == "" || !json.Valid([]byte(codex)) {
		t.Errorf("codex: want a JSON allow, got %q", codex)
	}
}

func TestRunPreCompact_BadPayloadAllows(t *testing.T) {
	if err := runPreCompact([]byte("not json")); err != nil {
		t.Fatalf("a bad payload must never fail the hook: %v", err)
	}
}
