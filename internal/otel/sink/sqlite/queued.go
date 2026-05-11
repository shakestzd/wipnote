package sqlite

import (
	"context"
	"errors"

	"github.com/shakestzd/wipnote/internal/db/writequeue"
	"github.com/shakestzd/wipnote/internal/otel"
	"github.com/shakestzd/wipnote/internal/otel/sink"
)

// QueuedSink is a sink.SignalSink that routes every WriteBatch through
// the single-writer queue introduced by slice 6 (feat-f3bcbcef,
// plan-ae0c37b2). It exists so that the indexer + OTLP HTTP receiver
// share exactly one writable SQLite connection — the queue worker's —
// instead of opening their own (which is what created the SQLITE_BUSY
// contention the plan targets).
//
// CANONICAL-FIRST CONTRACT:
//
//	Producers MUST persist user work to canonical NDJSON/HTML BEFORE
//	calling WriteBatch on this sink. Specifically:
//
//	  - The indexer reads NDJSON as its input — the canonical write
//	    happened upstream by the per-session collector. Submitting to
//	    the queue is purely a derived-index update.
//
//	  - The OTLP HTTP receiver, when wired in serve, must first append
//	    canonical NDJSON via the ndjson sink, THEN call this sink. We
//	    document the contract here so the upcoming receiver wiring
//	    follows the rule.
//
// On queue-side errors (ErrQueueFull / ErrTimeout / ErrWriterUnavailable)
// WriteBatch returns nil — the canonical write already preserved user
// work, and propagating an error here would tempt callers to retry into
// an already-overloaded writer. The queue's Stats counters expose
// rejection metrics for the dashboard collector-status panel.
type QueuedSink struct {
	queue *writequeue.Queue
	inner WriterCloser
}

// NewQueued wraps inner in a queue-backed sink. The caller is
// responsible for starting the queue (q.Start) and the inner Writer's
// lifecycle (close on shutdown). Returns a *QueuedSink (not the
// interface) so callers can introspect the underlying queue for
// diagnostics.
func NewQueued(q *writequeue.Queue, inner WriterCloser) *QueuedSink {
	return &QueuedSink{queue: q, inner: inner}
}

// WriteBatch submits a closure-wrapped WriteBatch to the queue. The
// closure captures harness/resourceAttrs/signals so the consumer
// goroutine can run the actual write later without touching the
// producer's frame.
//
// Returns nil on queue rejection (canonical-first contract — see type
// doc). The op-side error path is observable through queue Stats.
func (s *QueuedSink) WriteBatch(ctx context.Context, harness otel.Harness, resourceAttrs map[string]any, signals []otel.UnifiedSignal) error {
	if len(signals) == 0 {
		return nil
	}
	op := func(opCtx context.Context) error {
		_, err := s.inner.WriteBatch(opCtx, harness, resourceAttrs, signals)
		return err
	}
	err := s.queue.Submit(ctx, op)
	if err == nil {
		return nil
	}
	// Best-effort fallback: queue full / writer unavailable / timeout
	// all map to "canonical NDJSON already won, drop the index update".
	// We return nil so the producer's Stop/Tick path treats the
	// canonical write as authoritative.
	if errors.Is(err, writequeue.ErrQueueFull) ||
		errors.Is(err, writequeue.ErrWriterUnavailable) ||
		errors.Is(err, writequeue.ErrTimeout) {
		return nil
	}
	// Anything else (e.g. ctx.Err() from a cancelled producer context)
	// surfaces to the caller verbatim.
	return err
}

// Close releases the underlying writer. The caller MUST stop the queue
// FIRST (via queue.Stop) so the consumer drains any pending ops before
// the writer's prepared statements are torn down — otherwise an
// in-flight op would see a closed *sql.Conn and fail.
func (s *QueuedSink) Close() error { return s.inner.Close() }

// Queue exposes the underlying queue so the dashboard collector-status
// handler can read Stats without holding a separate reference.
func (s *QueuedSink) Queue() *writequeue.Queue { return s.queue }

// Compile-time interface check.
var _ sink.SignalSink = (*QueuedSink)(nil)
