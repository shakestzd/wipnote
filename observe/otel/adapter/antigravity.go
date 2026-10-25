package adapter

import "github.com/shakestzd/wipnote/observe/otel"

// AntigravityAdapter converts Antigravity CLI OTel emissions into
// UnifiedSignals. Antigravity keeps Gemini CLI's telemetry names, so it uses
// the shared GenAI mapping (genai.go).
//
// Identification: resource service.name == "antigravity-cli".
type AntigravityAdapter struct{ genAIAdapter }

// NewAntigravityAdapter returns an AntigravityAdapter ready for use.
func NewAntigravityAdapter() *AntigravityAdapter {
	return &AntigravityAdapter{genAIAdapter{harness: otel.HarnessAntigravity}}
}
