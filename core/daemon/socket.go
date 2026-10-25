package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shakestzd/wipnote/core/db/writequeue"
)

// dedupCapacity bounds the in-memory LRU that tracks recently-applied
// op_ids. MVP-2 is in-memory ONLY — durable cross-restart dedup is slice-3
// scope (DEFERRED). 4096 covers a generous burst window for the hot path.
const dedupCapacity = 4096

// Applier turns a received Envelope into a writequeue.WriteOp. It runs on
// the listener goroutine (not the writer goroutine) and must be cheap +
// side-effect-free: the returned closure is what actually mutates the DB,
// and it executes later inside the single-writer queue. An unknown op_type
// returns an error, which the listener surfaces as an error ack (never a
// mis-applied write).
//
// MVP-2 ships no real appliers wired to callers — the listener is
// constructed with whatever Applier its owner provides (serve_child's
// headless mode, or a test). MVP-3 supplies the dbgate derived-op applier.
type Applier func(env Envelope) (writequeue.WriteOp, error)

// RejectingApplier is the MVP-2 default Applier: it rejects every op_type
// with an error so the headless writer-only mode can prove the transport
// (dial, version-check, dedup, ack) end-to-end WITHOUT applying any real
// write. MVP-3 replaces this with the dbgate derived-op applier. Callers
// that wire real ops must supply their own Applier.
func RejectingApplier(env Envelope) (writequeue.WriteOp, error) {
	return nil, fmt.Errorf("no applier registered for op_type %q (MVP-2 transport only)", env.OpType)
}

// Listener accepts versioned write-op envelopes on a per-project Unix
// socket and funnels each accepted op through the SAME single-writer
// mechanism serve_child already uses: an internal/db/writequeue.Queue.
//
// Every op is submitted with Queue.SubmitSync so the ack reflects the
// actual commit outcome (applied/error) rather than mere enqueue. This is
// the structural single-writer guarantee — all writes for a project DB
// serialize through one Queue consumer goroutine owned by one process
// (the lease holder), regardless of how many clients dial the socket.
type Listener struct {
	ln       net.Listener
	queue    *writequeue.Queue
	applier  Applier
	sockPath string

	// lease and sockInfo are what this listener owns: the writer lease (nil
	// for a fixture) and the socket inode it bound. See socket_life.go.
	lease    *Lease
	sockInfo fs.FileInfo

	// reader answers read-protocol frames (feat-f6759e37). Nil means this
	// daemon serves writes only, and every read is answered
	// ReadStatusUnsupported — never an empty result, so a client can tell
	// "no read path here" from "nothing matched".
	reader Reader

	// attachState tracks launcher pids that suppress idle-exit. See
	// socket_read.go for why the launcher's guarantee needs it.
	attachState

	// ready gates applied-ack writes while the owner's state is still
	// loading. Nil means always ready. See socket_ready.go.
	ready func() bool

	seq atomic.Int64

	// dedup tracks op_ids in TWO disjoint states under dedupMu (roborev-482
	// round-5 finding 1):
	//
	//   dedupApplied — op_ids whose apply has CONFIRMED-committed. A duplicate
	//     submit of an applied op_id returns a DURABLE AckDuplicate: the write is
	//     known to have landed, so collapsing the resubmit is correct.
	//
	//   dedupPending — async op_ids enqueued but NOT yet confirmed-applied. A
	//     duplicate submit of a PENDING op_id must NOT be acked as a durable
	//     duplicate: the first apply might still FAIL, and an already-acked
	//     duplicate that skipped its fallback would then be silently lost. Such a
	//     duplicate is RE-ENQUEUED instead (every async-routed op is idempotent —
	//     INSERT OR IGNORE / INSERT OR REPLACE / UPDATE / upsert — so a second
	//     apply of an identical op is harmless and guarantees the write lands even
	//     if the first apply fails).
	//
	// Lifecycle (async): enqueue → add to dedupPending. Apply success → move
	// pending→applied. Apply failure → drop from pending (a resubmit re-runs).
	// The sync path only ever touches dedupApplied (after SubmitSync confirms the
	// commit), so it has nothing pending to roll back.
	dedupMu      sync.Mutex
	dedupApplied *lruSet
	dedupPending *lruSet

	closeOnce sync.Once
	wg        sync.WaitGroup

	// Idle-exit accounting (feat-075c110d lifecycle hardening). lastActivity
	// is the UnixNano of the most recent op submission; inFlight counts ops
	// currently between accept and ack. ServeWithIdleTimeout exits only when
	// BOTH no op has arrived within the idle window AND inFlight == 0, so an
	// in-progress Submit can never race the idle shutdown.
	lastActivity atomic.Int64
	inFlight     atomic.Int64
}

// ListenerConfig configures a Listener. SocketPath is the Unix socket the
// listener binds (caller derives it via SocketPath(projectRoot)). Queue is
// the single-writer queue ops funnel through — the SAME type serve_child
// constructs. Applier maps op_type+payload to a WriteOp.
type ListenerConfig struct {
	SocketPath string
	Queue      *writequeue.Queue
	Applier    Applier

	// Lease is the writer lease this process holds (bug-5ca53f70). Only the
	// lease owner may bind the socket: NewListener refuses when Lease no
	// longer names this process, and the listener exits once it stops doing
	// so. A nil Lease is for in-process fixtures: it binds only when nobody
	// holds the project's lease.
	Lease *Lease

	// Reader answers read-protocol frames (feat-f6759e37). Optional: a nil
	// Reader yields a write-only daemon whose reads are all answered
	// ReadStatusUnsupported. Unlike Applier there is no "rejecting" default to
	// supply, because the unsupported reply IS the correct rejection and it
	// already distinguishes itself from an empty result on the wire.
	Reader Reader

	// Ready reports whether ops can be applied now. Optional: nil means
	// always ready. See socket_ready.go for what "not ready" changes.
	Ready func() bool
}

// NewListener binds the Unix socket and returns a Listener ready to Serve.
//
// The lease, not the socket inode, is the ownership authority. Before it
// binds, unlinkStaleSocket confirms this process may bind at all (it holds
// cfg.Lease, or nobody holds the lease) and removes a leftover socket file,
// since a Unix-socket bind fails with EADDRINUSE while the path exists.
// Go's unlink-on-close is turned off: Close removes the path only while it is
// still the inode this listener bound, so a server that lost its socket can
// never delete its successor's.
func NewListener(cfg ListenerConfig) (*Listener, error) {
	if cfg.Queue == nil {
		return nil, errors.New("daemon: NewListener requires a non-nil Queue")
	}
	if cfg.Applier == nil {
		return nil, errors.New("daemon: NewListener requires a non-nil Applier")
	}
	if err := unlinkStaleSocket(cfg.SocketPath, cfg.Lease); err != nil {
		return nil, err
	}
	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix %s: %w", cfg.SocketPath, err)
	}
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	sockInfo, _ := leaseFS.Stat(cfg.SocketPath)
	l := &Listener{
		ln:           ln,
		queue:        cfg.Queue,
		applier:      cfg.Applier,
		reader:       cfg.Reader,
		ready:        cfg.Ready,
		sockPath:     cfg.SocketPath,
		lease:        cfg.Lease,
		sockInfo:     sockInfo,
		dedupApplied: newLRUSet(dedupCapacity),
		dedupPending: newLRUSet(dedupCapacity),
	}
	l.lastActivity.Store(time.Now().UnixNano())
	return l, nil
}

// Serve accepts connections until the listener is closed or ctx is done.
// Each connection is handled on its own goroutine; a connection may carry
// multiple envelopes (one ack per envelope) so callers can reuse a dial.
func (l *Listener) Serve(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		_ = l.Close()
	}()
	for {
		conn, err := l.ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				// Wait for Close to finish removing our socket file, so the
				// path is gone by the time Serve returns.
				_ = l.Close()
				l.wg.Wait()
				return nil
			}
			return fmt.Errorf("accept: %w", err)
		}
		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			l.handleConn(ctx, conn)
		}()
	}
}

// handleConn reads newline-delimited frames and writes one reply per frame.
// The connection closes on read EOF or a malformed frame.
//
// A frame is either a write Envelope (replied to with an Ack) or a
// ReadRequest (replied to with a ReadResponse). isReadFrame discriminates on
// the "read_op" field before either decoder runs, so the two can never be
// confused — see readwire.go.
func (l *Listener) handleConn(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	encoder := json.NewEncoder(conn)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var reply any
			if isReadFrame(line) {
				reply = l.processRead(line)
			} else {
				reply = l.process(ctx, line)
			}
			if encErr := encoder.Encode(reply); encErr != nil {
				return // client went away
			}
		}
		if err != nil {
			return // EOF or read error — done with this connection
		}
	}
}

// process decodes one envelope, validates the version, dedups, and funnels
// the op through the writequeue. It always returns an Ack (never panics on
// bad input). A monotonic seq is assigned to every accepted submission.
func (l *Listener) process(ctx context.Context, line []byte) Ack {
	// Idle-exit accounting: an op is "in flight" for the whole decode→ack
	// span. Recording activity on BOTH entry and exit, plus the inFlight
	// counter, guarantees ServeWithIdleTimeout never declares the writer idle
	// while a Submit is mid-process (decode/apply/commit can outlast the idle
	// window for a slow op).
	l.inFlight.Add(1)
	l.lastActivity.Store(time.Now().UnixNano())
	defer func() {
		l.lastActivity.Store(time.Now().UnixNano())
		l.inFlight.Add(-1)
	}()

	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Ack{Status: AckError, Seq: l.seq.Add(1), Error: "malformed envelope: " + err.Error()}
	}
	// Version skew: reject unknown formats loudly rather than risk a
	// mis-applied write (slice-1 decision).
	if env.OpFormatVersion != OpFormatVersion {
		return Ack{
			Status: AckError,
			Seq:    l.seq.Add(1),
			Error:  fmt.Sprintf("unsupported op_format_version %d (daemon speaks %d)", env.OpFormatVersion, OpFormatVersion),
		}
	}

	// Only a CONFIRMED-APPLIED op_id is acked duplicate; see isApplied.
	if l.isApplied(env.OpID) {
		return Ack{Status: AckDuplicate, Seq: l.seq.Add(1)}
	}

	op, err := l.applier(env)
	if err != nil {
		return Ack{Status: AckError, Seq: l.seq.Add(1), Error: "apply " + env.OpType + ": " + err.Error()}
	}

	if ack, rejected := l.notReadyAck(env); rejected {
		return ack
	}

	// Both modes funnel through the SAME single-writer queue serve_child uses,
	// preserving FIFO ordering and the structural single-writer guarantee; they
	// differ ONLY in when the ack is sent relative to apply.
	if env.Async {
		return l.submitEnqueueOnly(ctx, env, op)
	}
	return l.submitAndAwaitApply(ctx, env, op)
}

// submitAndAwaitApply is the SYNCHRONOUS (default) path: it blocks on
// SubmitSync until the writer goroutine commits the op, so the ack reflects the
// real outcome. Queue rejections (full/unavailable) surface as an error ack —
// the caller (MVP-3/4 typed routes, applied-ack RouteSQL) decides whether to
// fall back to direct-open. This preserves the pre-existing typed-route
// semantics exactly.
func (l *Listener) submitAndAwaitApply(ctx context.Context, env Envelope, op writequeue.WriteOp) Ack {
	if err := l.queue.SubmitSync(ctx, op); err != nil {
		return Ack{Status: AckError, Seq: l.seq.Add(1), Error: "writequeue: " + err.Error()}
	}
	// Commit succeeded — record op_id in the APPLIED set so a later duplicate
	// durably dedups, then ack applied. The sync path never touches the pending
	// set: it records only AFTER the commit is confirmed, so there is nothing to
	// roll back (roborev-482 round-5 finding 1).
	seq := l.seq.Add(1)
	l.recordApplied(env.OpID)
	return Ack{Status: AckApplied, Seq: seq}
}

// submitEnqueueOnly is the ENQUEUE-ONLY (Envelope.Async) path: it hands the op
// to the queue via the non-blocking Submit and acks AckEnqueued the instant the
// op is durably queued — it does NOT wait for apply. A queue rejection
// (ErrQueueFull / writer-unavailable) is the ONLY synchronous failure surface
// here; it becomes an error ack so the caller falls back (canonical NDJSON +
// reindex is the backstop). The single-writer worker still applies the op in
// FIFO order after this returns. roborev 451/452: this is what keeps a hot hook
// under its <1s bound when another writer holds the lock.
//
// Pending-vs-applied dedup (roborev-482 round-5 finding 1, refining roborev-480
// finding 1): on enqueue we record the op_id in the PENDING set, NOT the applied
// set. A duplicate that arrives while this op is still queued/applying is found
// in pending and is RE-ENQUEUED by process() (it gets its own apply attempt)
// rather than acked as a durable AckDuplicate — so if THIS op's apply later
// FAILS, the duplicate's own attempt still lands the write instead of being
// silently lost. The op is wrapped so that:
//   - on apply SUCCESS the op_id is PROMOTED pending→applied, so a post-success
//     duplicate durably dedups (no needless re-apply);
//   - on apply FAILURE the op_id is DROPPED from pending, so a resubmit re-runs.
//
// Recording in pending BEFORE Submit means the worker's success-promotion /
// failure-drop can never lose its race against the record. If Submit itself
// fails we roll the pending entry back below.
//
// Idempotency justification: every async-routed op type is idempotent —
// agent_event.upsert is INSERT OR IGNORE (EnsureSession) + INSERT OR REPLACE
// (UpsertEvent); session.insert is an upsert; feature.status / session.status
// are UPDATEs; sql.exec carries a content-derived op_id (sqlOpID = hash of
// statement+args) so any two ops sharing an op_id run byte-identical
// parameterized SQL. Re-enqueueing a pending duplicate can therefore at worst
// apply the same idempotent mutation twice, never double-insert a distinct row.
func (l *Listener) submitEnqueueOnly(ctx context.Context, env Envelope, op writequeue.WriteOp) Ack {
	// Record the PENDING entry BEFORE submitting, not after. The worker runs on
	// its own goroutine and may apply (promoting on success / dropping on
	// failure) before this function returns; recording first means that
	// transition can never lose its race against the record. If Submit itself
	// fails we roll the pending entry back below.
	l.recordPending(env.OpID)

	wrapped := op
	if env.OpID != "" {
		opID := env.OpID
		wrapped = func(opCtx context.Context) error {
			err := op(opCtx)
			if err != nil {
				// Apply failed: drop the pending entry so a resubmit of this
				// op_id re-applies instead of being swallowed (roborev-480
				// finding 1).
				l.dropPending(opID)
				return err
			}
			// Apply succeeded: promote pending→applied so a later duplicate
			// durably dedups (roborev-482 round-5 finding 1).
			l.promotePendingToApplied(opID)
			return nil
		}
	}
	if err := l.queue.Submit(ctx, wrapped); err != nil {
		// Never durably queued — undo the speculative pending record so the
		// caller's resubmit (after its direct-write fallback) is not affected.
		l.dropPending(env.OpID)
		return Ack{Status: AckError, Seq: l.seq.Add(1), Error: "writequeue: " + err.Error()}
	}
	// Durably queued — the op_id is in pending so a replayed async op that
	// arrives before this one commits is re-enqueued (not durably deduped); ack
	// enqueued WITHOUT waiting for the worker to commit. The wrapper above
	// promotes the entry on success / drops it on failure.
	seq := l.seq.Add(1)
	return Ack{Status: AckEnqueued, Seq: seq}
}

// Close stops accepting connections, waits briefly for in-flight handlers,
// and removes the socket file. Idempotent.
func (l *Listener) Close() error {
	var err error
	l.closeOnce.Do(func() {
		err = l.ln.Close()
		// Unlink so the next owner binds cleanly, but only our own inode.
		if l.ownsSocket() {
			_ = leaseFS.Remove(l.sockPath)
		}
	})
	return err
}

// Addr returns the bound socket path (for diagnostics/tests).
func (l *Listener) Addr() string { return l.sockPath }

// drainTimeout is the grace period Close-via-context handlers get; kept
// here as a named constant in case a future graceful-shutdown path wants
// to bound the wait explicitly.
const drainTimeout = 2 * time.Second

var _ = drainTimeout
