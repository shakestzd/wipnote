package writequeue

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Sentinel errors returned by Submit / SubmitWithTimeout. Producers
// match against these with errors.Is and fall back to canonical-only
// persistence on any of them.
var (
	// ErrQueueFull means the bounded queue is at capacity and the
	// non-blocking Submit could not enqueue the op. The producer's
	// canonical NDJSON write is unaffected — the derived SQLite index
	// will recover on reindex.
	ErrQueueFull = errors.New("writequeue: queue full")

	// ErrTimeout means SubmitWithTimeout's deadline elapsed before a
	// slot opened in the queue. Semantically identical to ErrQueueFull
	// from the producer's perspective (best-effort fallback) — surfaced
	// separately so observability can distinguish "instantaneously full"
	// from "consumer too slow".
	ErrTimeout = errors.New("writequeue: submit timeout")

	// ErrWriterUnavailable means the queue has not been started or has
	// been stopped. Producers must treat this exactly like ErrQueueFull
	// — the canonical NDJSON write already happened, so user work is
	// preserved.
	ErrWriterUnavailable = errors.New("writequeue: writer unavailable")
)

// State labels the queue's lifecycle phase. Surfaced through Stats so
// the dashboard collector-status panel can distinguish a healthy
// writer from one mid-shutdown or never-started.
type State string

const (
	StateInit     State = "init"     // constructed, Start not yet called
	StateRunning  State = "running"  // consumer goroutine alive
	StateDraining State = "draining" // Stop called; consumer flushing remaining ops
	StateStopped  State = "stopped"  // consumer exited
)

// Stats is the diagnostic snapshot returned by Queue.Stats.
//
// Depth is the instantaneous in-flight op count (channel length).
// Enqueued / Dequeued / Rejected / Errors are monotonic counters.
// EnqueueRate / DequeueRate are computed over the most recent 60-second
// window so the dashboard sees fresh activity rather than a session-wide
// running average that smooths over bursts.
type Stats struct {
	State          State   `json:"state"`
	Depth          int     `json:"depth"`
	Capacity       int     `json:"capacity"`
	Enqueued       int64   `json:"enqueued"`
	Dequeued       int64   `json:"dequeued"`
	Rejected       int64   `json:"rejected"`
	Errors         int64   `json:"errors"`
	EnqueueRatePerSec float64 `json:"enqueue_rate_per_sec"`
	DequeueRatePerSec float64 `json:"dequeue_rate_per_sec"`
}

// Config controls the queue's bounded behaviour and the consumer's
// per-op error handling.
//
//	Capacity   — channel buffer size. Default 1000 ops. Tuned per
//	             the plan's burst-handling note: ~10x typical
//	             sub-agent SubagentStop concurrency.
//	OnError    — optional callback for op execution errors. Defaults
//	             to a no-op so tests can swap in a recorder; production
//	             wires this to a logger.
type Config struct {
	Capacity int
	OnError  func(error)
}

// DefaultCapacity is the queue size when Config.Capacity is unset.
const DefaultCapacity = 1000

// Queue serializes WriteOp execution behind a single consumer
// goroutine. Producers submit via Submit (non-blocking) or
// SubmitWithTimeout (bounded wait). The consumer drains the channel
// and runs each op against whatever writable DB handle the producer
// captured.
//
// Queue is safe for concurrent producers; it is NOT safe to construct
// more than one Queue per project DB (the architectural single-writer
// invariant). See package doc.
type Queue struct {
	cfg Config
	ch  chan WriteOp

	// state guards lifecycle transitions. The atomic.Int32 holds an
	// internal state code so Submit can fast-fail without taking a
	// mutex.
	stateMu sync.Mutex
	state   atomic.Int32

	// done is closed by the consumer goroutine when it exits.
	done chan struct{}

	// rate tracking — protected by rateMu.
	rateMu          sync.Mutex
	rateWindow      time.Duration
	enqueueHistory  []int64 // unix-nano timestamps of recent enqueues
	dequeueHistory  []int64 // unix-nano timestamps of recent dequeues

	// monotonic counters.
	enqueued atomic.Int64
	dequeued atomic.Int64
	rejected atomic.Int64
	errors   atomic.Int64
}

// state code constants — kept private so the public State type is the
// only thing callers see.
const (
	stateCodeInit int32 = iota
	stateCodeRunning
	stateCodeDraining
	stateCodeStopped
)

// New constructs a Queue with the given config. The queue is in
// StateInit until Start is called.
func New(cfg Config) *Queue {
	if cfg.Capacity <= 0 {
		cfg.Capacity = DefaultCapacity
	}
	if cfg.OnError == nil {
		cfg.OnError = func(error) {}
	}
	q := &Queue{
		cfg:        cfg,
		ch:         make(chan WriteOp, cfg.Capacity),
		done:       make(chan struct{}),
		rateWindow: 60 * time.Second,
	}
	q.state.Store(stateCodeInit)
	return q
}

// Start launches the single consumer goroutine. Idempotent: a second
// call after StateRunning is a no-op. Returns ErrWriterUnavailable if
// the queue has already been stopped (Queue instances are not
// reusable).
func (q *Queue) Start(ctx context.Context) error {
	q.stateMu.Lock()
	defer q.stateMu.Unlock()

	current := q.state.Load()
	if current == stateCodeRunning || current == stateCodeDraining {
		return nil
	}
	if current == stateCodeStopped {
		return ErrWriterUnavailable
	}
	q.state.Store(stateCodeRunning)
	go q.consume(ctx)
	return nil
}

// Stop signals the consumer to drain and exit. Blocks up to timeout
// for the in-flight ops to complete. Safe to call multiple times.
//
// After Stop, Submit returns ErrWriterUnavailable. The Queue cannot be
// restarted — construct a new instance.
func (q *Queue) Stop(timeout time.Duration) {
	q.stateMu.Lock()
	current := q.state.Load()
	if current != stateCodeRunning {
		q.stateMu.Unlock()
		return
	}
	q.state.Store(stateCodeDraining)
	close(q.ch)
	q.stateMu.Unlock()

	if timeout <= 0 {
		<-q.done
		return
	}
	select {
	case <-q.done:
	case <-time.After(timeout):
	}
}

// Submit enqueues op for asynchronous execution. Non-blocking: if the
// channel is at capacity, returns ErrQueueFull immediately. Producers
// MUST tolerate ErrQueueFull — the canonical NDJSON write already
// happened, so user work is safe.
//
// Returns ErrWriterUnavailable when the queue has not been Started or
// has been Stopped. Returns ctx.Err() if the context is already
// cancelled (mirrors stdlib idioms; lets producers stop submitting
// during shutdown).
func (q *Queue) Submit(ctx context.Context, op WriteOp) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current := q.state.Load()
	if current != stateCodeRunning {
		q.rejected.Add(1)
		return ErrWriterUnavailable
	}
	select {
	case q.ch <- op:
		q.enqueued.Add(1)
		q.recordEnqueue()
		return nil
	default:
		q.rejected.Add(1)
		return ErrQueueFull
	}
}

// SubmitWithTimeout enqueues op, waiting up to timeout for a slot to
// open. Returns ErrTimeout if the deadline elapses, ErrWriterUnavailable
// if the queue stops mid-wait, or ctx.Err() if the context is cancelled.
//
// Producers that prefer to drop on overflow should call Submit; this
// variant exists for low-priority background work that can afford to
// wait briefly when the writer is momentarily behind.
func (q *Queue) SubmitWithTimeout(ctx context.Context, op WriteOp, timeout time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current := q.state.Load()
	if current != stateCodeRunning {
		q.rejected.Add(1)
		return ErrWriterUnavailable
	}
	if timeout <= 0 {
		return q.Submit(ctx, op)
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case q.ch <- op:
		q.enqueued.Add(1)
		q.recordEnqueue()
		return nil
	case <-timer.C:
		q.rejected.Add(1)
		return ErrTimeout
	case <-ctx.Done():
		q.rejected.Add(1)
		return ctx.Err()
	}
}

// Stats returns a point-in-time snapshot of queue depth, lifecycle
// state, and counters. Safe to call from any goroutine.
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

// Capacity returns the configured queue capacity. Useful for tests
// that want to fill the queue exactly to its limit.
func (q *Queue) Capacity() int { return q.cfg.Capacity }

// consume is the single-writer consumer goroutine. Each op runs with
// the context passed to Start so the operator can cancel the entire
// writer service from one place.
func (q *Queue) consume(ctx context.Context) {
	defer close(q.done)
	for op := range q.ch {
		if op == nil {
			continue
		}
		// Run the op with the parent context. The op should respect
		// cancellation for its own internal DB calls; we don't try to
		// preempt it here because mid-transaction cancellation creates
		// rollback churn worse than just letting the op finish.
		if err := op(ctx); err != nil {
			q.errors.Add(1)
			q.cfg.OnError(err)
		}
		q.dequeued.Add(1)
		q.recordDequeue()
	}
	q.state.Store(stateCodeStopped)
}

func (q *Queue) currentState() State {
	switch q.state.Load() {
	case stateCodeInit:
		return StateInit
	case stateCodeRunning:
		return StateRunning
	case stateCodeDraining:
		return StateDraining
	case stateCodeStopped:
		return StateStopped
	}
	return StateInit
}

// recordEnqueue / recordDequeue append a unix-nano timestamp to the
// rolling-window history so Stats can compute a meaningful rate even
// when the queue has been running for hours.
func (q *Queue) recordEnqueue() {
	q.rateMu.Lock()
	defer q.rateMu.Unlock()
	q.enqueueHistory = append(q.enqueueHistory, time.Now().UnixNano())
	q.trimHistoryLocked(&q.enqueueHistory)
}

func (q *Queue) recordDequeue() {
	q.rateMu.Lock()
	defer q.rateMu.Unlock()
	q.dequeueHistory = append(q.dequeueHistory, time.Now().UnixNano())
	q.trimHistoryLocked(&q.dequeueHistory)
}

func (q *Queue) trimHistoryLocked(hist *[]int64) {
	cutoff := time.Now().Add(-q.rateWindow).UnixNano()
	// Find first entry younger than cutoff.
	i := 0
	for i < len(*hist) && (*hist)[i] < cutoff {
		i++
	}
	if i > 0 {
		*hist = (*hist)[i:]
	}
}

// rate computes the per-second rate over the window. enqueue=true
// selects the enqueue history; false selects the dequeue history.
func (q *Queue) rate(enqueue bool) float64 {
	q.rateMu.Lock()
	defer q.rateMu.Unlock()
	var hist []int64
	if enqueue {
		hist = q.enqueueHistory
	} else {
		hist = q.dequeueHistory
	}
	if len(hist) == 0 {
		return 0
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
