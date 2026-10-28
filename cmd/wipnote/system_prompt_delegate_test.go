package main

import (
	"strings"
	"testing"
)

// The system prompt sends every hand-off to another harness through
// wipnote delegate, never a bare codex, claude -p or ccx call.
func TestSystemPrompt_DelegateGuidance(t *testing.T) {
	for _, want := range []string{
		"wipnote delegate run", "wipnote delegate spawn", "wipnote skill show delegate",
		"Do not call `codex exec`, `codex review`, `claude -p` or `ccx task` directly",
	} {
		if !strings.Contains(systemPromptContent, want) {
			t.Errorf("system prompt lacks %q", want)
		}
	}
	if strings.Contains(systemPromptContent, "preview, one-shot only") {
		t.Error("system prompt still calls delegate a one-shot preview")
	}
}
