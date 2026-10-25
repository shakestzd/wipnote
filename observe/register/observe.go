// Package observe holds the otel- and pluginbuild-backed implementations of the
// core hooks lifecycle injection points (feat-331927fb).
//
// It is intentionally NOT part of the core hooks surface: it imports telemetry
// (internal/otel/*) and plugin-build tooling (port/pluginbuild), neither of
// which may live in core. Blank-importing this package from the binary
// registers these implementations into core hooks at init time, so lifecycle
// hooks invoke them without taking a direct dependency on otel/pluginbuild.
package observe

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/observe/otel/collector"
	"github.com/shakestzd/wipnote/observe/otel/finalize"
	"github.com/shakestzd/wipnote/port/pluginbuild"
)

func init() {
	hooks.RetentionSweepFn = runRetentionSweep
	hooks.SessionTelemetryFn = finalizeSessionTelemetry
	hooks.PortDriftPathsFn = portDriftPaths
	hooks.ReapCollectorFn = collector.ReapCollector
}

// sessionStartCatchUpBudget bounds telemetry catch-up inside the SessionStart
// retention goroutine. That goroutine dies with the hook process, so the
// budget is small; the writer daemon's sweep and `wipnote prune` run the same
// catch-up with a larger budget.
const sessionStartCatchUpBudget = 2 * time.Second

// finalizeSessionTelemetry ingests the session's NDJSON, materializes its
// rollup and marks it consumed, all within budget. The hook passes the harness
// session id, but the launcher keys the telemetry directory by its own OTel id
// (WIPNOTE_OTEL_SESSION_ID), so every matching shard is finalized.
func finalizeSessionTelemetry(projectDir, sessionID string, budget time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	var errs []error
	for _, shard := range finalize.ShardsFor(projectDir, sessionID, os.Getenv("WIPNOTE_OTEL_SESSION_ID")) {
		if _, err := finalize.Session(ctx, projectDir, shard); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// runRetentionSweep performs a disk-retention pass for the project: finalize
// telemetry for sessions that never got a clean SessionEnd, rotate oversized
// logs, and archive+prune raw events.ndjson for inactive, fully-ingested
// sessions. Invoked fire-and-forget from session-start so disk reclamation
// happens at a natural lifecycle point.
//
// The DB handle is intentionally nil: the ndjson coverage sweep relies on the
// per-session .index-offset checkpoint, not the DB, and log rotation needs no
// DB. All errors are swallowed — retention must never affect session start.
func runRetentionSweep(projectDir, activeSessionID string) {
	if projectDir == "" {
		return
	}
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	_, _ = finalize.Sweep(nil, wipnoteDir, activeSessionID, false, sessionStartCatchUpBudget)
}

// portDriftPaths reuses the generator drift gate (pluginbuild.CheckPorts) to
// return the drifted generated-port paths for the plugin-core repo at repoRoot,
// or nil when in sync, not a plugin-core repo, or on any error. The generator
// is the single source of truth; this never reimplements port diffing.
func portDriftPaths(repoRoot string) []string {
	if repoRoot == "" {
		return nil
	}
	manifestPath := filepath.Join(repoRoot, "packages", "plugin-core", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		// Not a plugin-core repo (e.g. a downstream project dogfooding wipnote).
		return nil
	}
	m, err := pluginbuild.Load(manifestPath)
	if err != nil {
		return nil
	}
	drifts, err := pluginbuild.CheckPorts(m, repoRoot, pluginbuild.Names())
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(drifts))
	for _, d := range drifts {
		out = append(out, d.Path)
	}
	return out
}
