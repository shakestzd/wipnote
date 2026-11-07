package session

import (
	"context"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// WatchNone is the only line of a watch over no live workers.
const WatchNone = "watch: no live delegate workers; spawn one, then run delegate watch again"

// WatchAll is Watch over every open interactive worker at once: one line per
// worker state change, the same key=value lines, and workers spawned later
// join on the next poll. A worker that settles (anything but asks) prints its
// line and is no longer watched. The watch ends when none is left to watch or
// at the timeout, whose result carries exit status 124. It writes no run state.
func WatchAll(ctx context.Context, d Deps, o WaitOptions, emit func(line string)) (WaitResult, error) {
	if o.Timeout <= 0 {
		return WaitResult{}, ErrNoTimeout
	}
	poll := o.Poll
	if poll <= 0 {
		poll = DefaultWaitPoll
	}
	deadline := time.Now().Add(o.Timeout)
	look, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	last, finished := map[string]string{}, map[string]bool{}
	for first := true; ; first = false {
		watched, err := watchRound(look, d, last, finished, emit)
		if err != nil {
			return WaitResult{}, err
		}
		if err := ctx.Err(); err != nil {
			return WaitResult{}, err
		}
		if watched == 0 {
			if first {
				emit(WatchNone)
			}
			return WaitResult{Outcome: OutcomeStopped}, nil
		}
		left := time.Until(deadline)
		if left <= 0 || look.Err() != nil {
			emit(fmt.Sprintf("state=timeout workers=%d", watched))
			emit(WatchRerun)
			return WaitResult{Outcome: OutcomeTimeout}, nil
		}
		if err := sleepCtx(ctx, min(poll, left)); err != nil {
			return WaitResult{}, err
		}
	}
}

// watchRound looks at every open worker not yet finished once, emitting the
// lines that changed, and returns how many are still watched.
func watchRound(ctx context.Context, d Deps, last map[string]string, finished map[string]bool, emit func(string)) (int, error) {
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return 0, err
	}
	watched := 0
	for _, r := range recs {
		if r.Mode != delegationledger.ModeInteractive || !r.IsOpen() || finished[r.ID] {
			continue
		}
		res, settled := waitOnce(ctx, d, r.ID, r.Name)
		if ctx.Err() != nil {
			settled = false
		}
		if key := watchKey(res, settled); key != last[r.ID] {
			last[r.ID] = key
			emit(WatchLine(res, settled))
		}
		if settled && res.Outcome != OutcomeAsks {
			finished[r.ID] = true
			continue
		}
		watched++
	}
	return watched, nil
}
