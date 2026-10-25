package adapter

import "github.com/shakestzd/wipnote/observe/otel"

// GeminiAdapter converts Gemini CLI OTel emissions into UnifiedSignals using
// the shared GenAI mapping (genai.go).
//
// Identification: resource service.name == "gemini-cli".
//
// Canonical attribute mapping:
//
//	service.name=gemini-cli   session.id        → SessionID
//	gen_ai.prompt_id          → PromptID (when present)
type GeminiAdapter struct{ genAIAdapter }

// NewGeminiAdapter returns a GeminiAdapter ready for use.
func NewGeminiAdapter() *GeminiAdapter {
	return &GeminiAdapter{genAIAdapter{harness: otel.HarnessGemini}}
}
