package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClaudeSessionNameLastAgentName(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-some-project")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"user","message":"hi"}
{"type":"agent-name","agentName":"old-name","sessionId":"s1"}
{"type":"agent-name","agentName":"feat-1234abcd-new","sessionId":"s1"}
`
	if err := os.WriteFile(filepath.Join(proj, "s1.jsonl"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := claudeSessionName(dir, "s1"); got != "feat-1234abcd-new" {
		t.Errorf("got %q", got)
	}
	if got := claudeSessionName(dir, "missing"); got != "" {
		t.Errorf("missing: got %q", got)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Dir(dir))
	if got := claudeSessionName(dir, "../s1"); got != "" {
		t.Errorf("path id: got %q", got)
	}
}
