package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/shakestzd/htmlgraph/internal/otel/collector"
)

// appendOrReplaceEnv takes an env slice (KEY=VALUE strings) and one or more
// kv pairs in "KEY=VALUE" form. For each kv, if the key already exists in env
// its entry is replaced in-place; otherwise the kv is appended. The original
// slice is modified and returned. Order of existing entries is preserved.
func appendOrReplaceEnv(env []string, kv ...string) []string {
	for _, pair := range kv {
		key := pair
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			key = pair[:idx+1] // includes '=' so prefix matching works
		}
		replaced := false
		for i, e := range env {
			if strings.HasPrefix(e, key) {
				env[i] = pair
				replaced = true
				break
			}
		}
		if !replaced {
			env = append(env, pair)
		}
	}
	return env
}

// spawnCodexOtelCollector spawns a per-session OTel collector and returns the
// port, session ID, and a cleanup function. On failure it writes a warning to
// stderr and returns zero port / nil cleanup so the caller can proceed without
// telemetry. Exits non-zero when HTMLGRAPH_OTEL_STRICT=1 and spawn fails.
func spawnCodexOtelCollector(projectDir string) (port int, sessionID string, cleanup func()) {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "htmlgraph: warning: codex per-session collector skipped: %v\n", err)
		return 0, "", nil
	}

	sessionID = generateOtelSessionID()
	lc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr:     os.Stderr,
		StrictMode: os.Getenv("HTMLGRAPH_OTEL_STRICT") == "1",
	})

	spawnedPort, spawnCleanup, spawnErr := lc.Spawn(binPath, sessionID, projectDir)
	if spawnErr != nil {
		fmt.Fprintf(os.Stderr, "htmlgraph: FATAL: codex collector spawn failed: %v\n", spawnErr)
		if os.Getenv("HTMLGRAPH_OTEL_STRICT") == "1" {
			os.Exit(1)
		}
		return 0, "", nil
	}

	return spawnedPort, sessionID, spawnCleanup
}

// buildCodexOtelEnv returns a copy of base with OTel exporter variables set
// for the Codex CLI child process. port and sessionID come from
// spawnCodexOtelCollector; when port is 0 the base env is returned unchanged.
func buildCodexOtelEnv(base []string, port int, sessionID string) []string {
	if port == 0 {
		return base
	}
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	env := make([]string, len(base))
	copy(env, base)
	env = appendOrReplaceEnv(env,
		"OTEL_EXPORTER_OTLP_ENDPOINT="+endpoint,
		"OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf",
		"OTEL_SERVICE_NAME=codex-cli",
		"HTMLGRAPH_OTEL_SESSION="+sessionID,
	)
	return env
}
