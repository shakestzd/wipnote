package writequeue

import "time"

// Stats returns a point-in-time snapshot of queue depth, lifecycle state and
// counters, for tests that assert on queue accounting.
func (q *Queue) Stats() Stats {
	return Stats{
		State:             q.currentState(),
		Depth:             len(q.ch),
		Capacity:          q.cfg.Capacity,
		Enqueued:          q.enqueued.Load(),
		Dequeued:          q.dequeued.Load(),
		Rejected:          q.rejected.Load(),
		Errors:            q.errors.Load(),
		EnqueueRatePerSec: q.rate(true),
		DequeueRatePerSec: q.rate(false),
	}
}

func (q *Queue) currentState() State {
	switch q.state.Load() {
	case stateCodeRunning:
		return StateRunning
	case stateCodeDraining:
		return StateDraining
	case stateCodeStopped:
		return StateStopped
	}
	return StateInit
}

// rate is the per-second enqueue (or dequeue) rate over the rolling window.
func (q *Queue) rate(enqueue bool) float64 {
	q.rateMu.Lock()
	defer q.rateMu.Unlock()
	hist := q.dequeueHistory
	if enqueue {
		hist = q.enqueueHistory
	}
	cutoff := time.Now().Add(-q.rateWindow).UnixNano()
	count := 0
	for _, ts := range hist {
		if ts >= cutoff {
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return float64(count) / q.rateWindow.Seconds()
}
