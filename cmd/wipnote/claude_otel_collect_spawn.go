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
	// EndpointPath is appended to the collector's base URL: "/s/<session-id>"
	// on the shared collector, "" on a per-session one.
	EndpointPath string
	Cleanup      func() // called on launcher exit: detach or SIGTERM the collector
}

// spawnCollectorFn is the package-level spawn function used by retry and
// watchdog paths in this shim. Tests may replace it to inject a fake.
var spawnCollectorFn collector.SpawnFn = collector.DefaultSpawnFn

// spawnSharedCollectorFn starts the project's shared collector; tests may
// replace it.
var spawnSharedCollectorFn collector.SharedSpawnFn = collector.DefaultSharedSpawnFn

// sharedCollectorEnabled reports whether launches share one collector per
// project (issue #183). WIPNOTE_OTEL_SHARED=0 restores one per session.
func sharedCollectorEnabled() bool {
	return !isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_SHARED"))
}

// attachSharedCollectorTo attaches this launch to the project's shared
// collector, starting it when none answers.
func attachSharedCollectorTo(projectDir, binPath, sessionID string, errW io.Writer) (otelEnvOverrides, error) {
	sc := collector.NewSharedCollector(collector.SharedCollectorOpts{
		Stderr:  errW,
		SpawnFn: spawnSharedCollectorFn,
	})
	port, cleanup, err := sc.Spawn(binPath, sessionID, projectDir)
	if err != nil {
		return otelEnvOverrides{}, err
	}
	return otelEnvOverrides{
		CollectorPort: port,
		SessionID:     sessionID,
		EndpointPath:  collector.SessionEndpointPath(sessionID),
		Cleanup:       cleanup,
	}, nil
}

// spawnSessionCollectorTo is the testable core of collector spawning. It
// first attaches to the project's shared collector; only when that fails does
// it fall back to a per-session one. The fallback constructs a
// ProcessCollector configured with the package-level spawnCollectorFn (so
// tests can inject fakes via that variable) and calls
// Spawn. On spawn failure, returns an empty overrides and wantExit=true
// when WIPNOTE_OTEL_STRICT=1.
func spawnSessionCollectorTo(projectDir, binPath string, errW io.Writer) (otelEnvOverrides, bool) {
	sessionID := generateOtelSessionID()
	if sharedCollectorEnabled() {
		overrides, err := attachSharedCollectorTo(projectDir, binPath, sessionID, errW)
		if err == nil {
			return overrides, false
		}
		fmt.Fprintf(errW, "wipnote: warning: shared collector unavailable, starting one for this session: %v\n", err)
	}
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
