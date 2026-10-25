package hooks

import (
	"time"
)

// Lifecycle injection points (feat-331927fb).
//
// These package-level function vars are the seam that lets the core hook
// handlers stay free of telemetry (otel) and port-generation tooling. Core
// hooks call them only when non-nil; the concrete implementations live in
// the non-core sibling package internal/observe, which registers them in
// init(). The binary wires them in by blank-importing that package.
//
// A nil var means "feature not wired" — the core handler degrades to a no-op,
// which matches the best-effort, never-block semantics of these side concerns
// (disk retention, telemetry rollup, plugin-port drift). This is what keeps the
// core hook surface importable from core/ once the package is split out.
var (
	// RetentionSweepFn runs the fire-and-forget disk-retention pass at session
	// start (log rotation + ndjson archival for inactive sessions).
	RetentionSweepFn func(projectDir, activeSessionID string)

	// SessionTelemetryFn finalizes a session's collector telemetry at session
	// end, within budget: it ingests events.ndjson, materializes the OTel
	// rollup, and only then publishes .index-offset so retention may archive
	// the raw log. Registered from observe/otel/finalize. A spent budget leaves
	// the session for the catch-up pass that runs before every retention sweep.
	SessionTelemetryFn func(projectDir, sessionID string, budget time.Duration) error

	// PortDriftPathsFn returns the generated-plugin-tree paths that have drifted
	// from the manifest for the repo rooted at repoRoot, or nil when in sync,
	// not a plugin-core repo, or the checker is unavailable. Shared by the
	// session-exit reconcile and the pre-commit port-drift guard.
	PortDriftPathsFn func(repoRoot string) []string

	// CollectorAliveFn reports whether the .collector-pid under sessDir names a
	// live collector, parsing the writer's "<pid>\n<starttime>" format and
	// checking the start time against pid reuse. Wired to
	// collector.IsCollectorAlive by observe/register. nil falls back to reading
	// the pid line alone.
	CollectorAliveFn func(sessDir string) (alive bool, pid int)
)
