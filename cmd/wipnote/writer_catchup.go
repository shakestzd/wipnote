package main

import (
	"context"
	"sync"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/finalize"
)

// telemetryCatchUpInterval is how often the writer daemon finalizes telemetry
// shards that never got a clean SessionEnd. Catch-up itself skips logs written
// within retention.ActiveGrace, so a fresh shard waits for a later pass.
const telemetryCatchUpInterval = 10 * time.Minute

// catchUpMu serializes catch-up runs so two never overlap.
var catchUpMu sync.Mutex

// runTelemetryCatchUp runs one catch-up pass bounded by telemetryCatchUpBudget
// and by ctx (daemon shutdown). It reports false, doing nothing, when another
// pass is already running.
func runTelemetryCatchUp(ctx context.Context, projectRoot string) bool {
	if !catchUpMu.TryLock() {
		return false
	}
	defer catchUpMu.Unlock()
	runCtx, cancel := context.WithTimeout(ctx, telemetryCatchUpBudget)
	defer cancel()
	finalize.CatchUp(runCtx, projectRoot, "")
	return true
}

// startTelemetryCatchUpLoop runs catch-up at startup and then every interval,
// on the daemon's shared drain-loop scheduler, until ctx is cancelled.
func startTelemetryCatchUpLoop(ctx context.Context, interval time.Duration, projectRoot string) {
	startCatchUpLoop(ctx, interval, func(c context.Context) {
		runTelemetryCatchUp(c, projectRoot)
	})
}

// startCatchUpLoop adapts fn to startDrainLoop, which runs passes one at a
// time and stops on ctx cancellation.
func startCatchUpLoop(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	startDrainLoop(ctx, interval, func() {
		if ctx.Err() == nil {
			fn(ctx)
		}
	})
}
