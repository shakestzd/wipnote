package main

import (
	"strings"
	"testing"
)

// The system prompt sends every hand-off to another harness through
// wipnote delegate, never a bare codex, claude -p or ccx call.
func TestSystemPrompt_DelegateGuidance(t *testing.T) {
	for _, want := range []string{
		"wipnote delegate --help",
		"Do not call `codex exec`, `claude -p` or `ccx task` directly",
	} {
		if !strings.Contains(systemPromptContent, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	// The steps live in the command help, not in the prompt.
	for _, gone := range []string{"wipnote delegate spawn", "herdr agent start"} {
		if strings.Contains(systemPromptContent, gone) {
			t.Errorf("system prompt still carries %q", gone)
		}
	}
	if strings.Contains(systemPromptContent, "preview, one-shot only") {
		t.Error("system prompt still calls delegate a one-shot preview")
	}
}
