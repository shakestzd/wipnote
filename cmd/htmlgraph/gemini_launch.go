package main

import (
	"fmt"
	"os"

	"github.com/shakestzd/htmlgraph/internal/otel/collector"
)

// spawnGeminiOtelCollector spawns a per-session OTel collector and returns the
// port, session ID, and a cleanup function. On failure it writes a warning to
// stderr and returns zero port / nil cleanup so the caller can proceed without
// telemetry. Exits non-zero when HTMLGRAPH_OTEL_STRICT=1 and spawn fails.
func spawnGeminiOtelCollector(projectDir string) (port int, sessionID string, cleanup func()) {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "htmlgraph: warning: gemini per-session collector skipped: %v\n", err)
		return 0, "", nil
	}

	sessionID = generateOtelSessionID()
	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr:     os.Stderr,
		StrictMode: os.Getenv("HTMLGRAPH_OTEL_STRICT") == "1",
	})

	spawnedPort, spawnCleanup, spawnErr := lc.Spawn(binPath, sessionID, projectDir)
	if spawnErr != nil {
		fmt.Fprintf(os.Stderr, "htmlgraph: FATAL: gemini collector spawn failed: %v\n", spawnErr)
		if os.Getenv("HTMLGRAPH_OTEL_STRICT") == "1" {
			os.Exit(1)
		}
		return 0, "", nil
	}

	return spawnedPort, sessionID, spawnCleanup
}

// buildGeminiOtelEnv returns a copy of base with Gemini telemetry variables set
// for the Gemini CLI child process. port and sessionID come from
// spawnGeminiOtelCollector; when port is 0 the base env is returned unchanged.
func buildGeminiOtelEnv(base []string, port int, sessionID string) []string {
	if port == 0 {
		return base
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	env := make([]string, len(base))
	copy(env, base)
	env = appendOrReplaceEnv(env,
		"GEMINI_TELEMETRY_ENABLED=true",
		"GEMINI_TELEMETRY_USE_COLLECTOR=true",
		"GEMINI_TELEMETRY_OTLP_ENDPOINT="+endpoint,
		"GEMINI_TELEMETRY_OTLP_PROTOCOL=http",
		"HTMLGRAPH_OTEL_SESSION="+sessionID,
	)
	return env
}
