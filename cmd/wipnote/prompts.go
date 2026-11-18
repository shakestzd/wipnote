package main

import (
	_ "embed"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

//go:embed prompts/system-prompt.md
var systemPromptContent string

//go:embed prompts/yolo-prompt.md
var yoloPromptContent string

//go:embed prompts/gemini-system.md
var geminiSystemPrompt string

//go:embed prompts/research-routing.md
var researchRoutingContent string

//go:embed prompts/worker-rules.md
var workerRulesContent string

func init() { session.WorkerRules = workerRulesContent }
