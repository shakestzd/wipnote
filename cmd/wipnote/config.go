package main

import (
	"context"
	"time"
)

// reconcileDrainInterval is how often the writer daemon auto-commits
// done-but-uncommitted work-item artifacts (feat-c08d1ba1 slice-6). The Stop
// hook stopped running this class synchronously because its per-item git fork
// loop was a ~5.45s per-turn cost; this out-of-band loop performs the same
// deterministic auto-commit off the interactive path so the durable record is
// still committed, just not on a model-response boundary.
const reconcileDrainInterval = 5 * time.Minute

// startDrainLoop runs fn once at startup and then on every interval tick inside
// the headless writer daemon, where wall-clock cost is not user-visible. A panic
// in fn is recovered so one malformed artifact can never crash the writer daemon.
// The loop stops on ctx cancellation. Shared by the reconcile drain and the
// telemetry catch-up loop (feat-c08d1ba1).
func startDrainLoop(ctx context.Context, interval time.Duration, fn func()) {
	drain := func() {
		defer func() { _ = recover() }()
		fn()
	}
	go func() {
		drain()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				drain()
			}
		}
	}()
}
