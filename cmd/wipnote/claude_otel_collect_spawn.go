package main

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// generateOtelSessionID produces a hex session ID from a Unix-millisecond
// timestamp (12 hex digits) and 8 random bytes (16 hex digits), giving
// 28 hex characters total. Lexicographically sortable by creation time.
// Distinct from generateSessionID (sess-{hex8}) which is used for
// non-OTel session tracking.
func generateOtelSessionID() string {
	ts := time.Now().UnixMilli()
	var entropy [8]byte
	_, _ = rand.Read(entropy[:]) // crypto/rand never errors on supported platforms
	return fmt.Sprintf("%012x%016x", ts, entropy)
}

// otelEnvOverrides holds optional overrides for OTel env vars set by
// the launcher. Zero-value fields mean "use the default derivation".
type otelEnvOverrides struct {
	CollectorPort int
	SessionID     string
	Cleanup       func() // called on launcher exit to SIGTERM the collector
}

// spawnCollectorFn is the package-level spawn function used by retry and
// watchdog paths in this shim. Tests may replace it to inject a fake.
var spawnCollectorFn collector.SpawnFn = collector.DefaultSpawnFn

// spawnSessionCollectorTo is the testable core of collector spawning. It
// constructs a ProcessCollector configured with the package-level
// spawnCollectorFn (so tests can inject fakes via that variable) and calls
// Spawn. On spawn failure, returns an empty overrides and wantExit=true
// when WIPNOTE_OTEL_STRICT=1.
func spawnSessionCollectorTo(projectDir, binPath string, errW io.Writer) (otelEnvOverrides, bool) {
	sessionID := generateOtelSessionID()
	pc := collector.NewProcessCollector(collector.ProcessCollectorOpts{
		Stderr:  errW,
		SpawnFn: spawnCollectorFn,
	})

	port, cleanup, err := pc.Spawn(binPath, sessionID, projectDir)
	if err != nil {
		wantExit := os.Getenv("WIPNOTE_OTEL_STRICT") == "1"
		return otelEnvOverrides{}, wantExit
	}

	return otelEnvOverrides{
		CollectorPort: port,
		SessionID:     sessionID,
		Cleanup:       cleanup,
	}, false
}

// spawnSessionCollector generates a session ID, spawns a per-session
// collector, writes the PID file, and returns a cleanup function.
// On spawn failure emits a FATAL line to stderr; exits non-zero when
// WIPNOTE_OTEL_STRICT=1. Silent-fail is preserved when the binary
// path cannot be resolved (soft precondition).
func spawnSessionCollector(projectDir string) otelEnvOverrides {
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "wipnote: warning: per-session collector skipped: %v\n", err)
		return otelEnvOverrides{}
	}

	overrides, wantExit := spawnSessionCollectorTo(projectDir, binPath, os.Stderr)
	if wantExit {
		os.Exit(1)
	}
	return overrides
}
