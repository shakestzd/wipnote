package main

import (
	"os"
	"path/filepath"
	"strings"
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

// The name is read from the end of a large transcript, where Claude Code
// re-writes it, without scanning the whole file.
func TestClaudeSessionNameReadsTheTail(t *testing.T) {
	dir := t.TempDir()
	proj := filepath.Join(dir, "-p")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	b.WriteString(`{"type":"agent-name","agentName":"early-name","sessionId":"s2"}` + "\n")
	pad := `{"type":"user","message":"` + strings.Repeat("x", 4096) + `"}` + "\n"
	for b.Len() < 3*tailBytes {
		b.WriteString(pad)
	}
	b.WriteString(`{"type":"agent-name","agentName":"late-name","sessionId":"s2"}` + "\n")
	b.WriteString(pad)
	if err := os.WriteFile(filepath.Join(proj, "s2.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := claudeSessionName(dir, "s2"); got != "late-name" {
		t.Errorf("got %q, want late-name", got)
	}
	// A name only near the start is still found by the full-file fallback.
	small := `{"type":"agent-name","agentName":"only-early","sessionId":"s3"}` + "\n" + strings.Repeat(pad, (3*tailBytes)/len(pad))
	if err := os.WriteFile(filepath.Join(proj, "s3.jsonl"), []byte(small), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := claudeSessionName(dir, "s3"); got != "only-early" {
		t.Errorf("fallback: got %q", got)
	}
}
