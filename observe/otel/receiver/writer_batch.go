package receiver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/observe/otel"
)

// WriteBatch persists one OTLP request's worth of signals plus the
// resource attributes that produced them. The whole batch runs in one
// BEGIN IMMEDIATE transaction — either every signal lands or none do.
//
// session_ids are deduplicated inside the transaction so we only issue
// one sessions placeholder upsert per distinct session in the batch.
//
// Returns the number of rows actually inserted (excludes idempotent
// rejections on duplicate signal_id). Callers log the rejection count
// separately for observability.
func (w *Writer) WriteBatch(
	ctx context.Context,
	harness otel.Harness,
	resourceAttrs map[string]any,
	signals []otel.UnifiedSignal,
) (inserted int, err error) {
	if len(signals) == 0 {
		return 0, nil
	}

	// Slice-10 contention observability: classify any error returned from
	// this WriteBatch under the writer_service subsystem. The launch gate
	// asserts this counter remains zero across the contention stress
	// fixture. See internal/db/busy_counter.go for the subsystem taxonomy.
	defer func() {
		dbpkg.Record(dbpkg.SubsystemWriterService, err)
	}()

	w.mu.Lock()
	defer w.mu.Unlock()

	err = dbpkg.RetryOnBusy(dbpkg.DefaultBusyBackoff, func() error {
		var attemptInserted int
		attemptInserted, err = w.writeBatchAttempt(ctx, harness, resourceAttrs, signals)
		if err != nil {
			return err
		}
		inserted = attemptInserted
		return nil
	})
	return inserted, err
}

func (w *Writer) writeBatchAttempt(
	ctx context.Context,
	harness otel.Harness,
	resourceAttrs map[string]any,
	signals []otel.UnifiedSignal,
) (inserted int, err error) {
	conn, stmts, release, err := w.batchConn(ctx)
	if err != nil {
		return 0, err
	}
	defer release()

	// BEGIN IMMEDIATE acquires the write lock up front, avoiding the
	// SHARED→RESERVED→EXCLUSIVE upgrade race that a DEFERRED transaction
	// triggers. With DEFERRED, SQLite holds only a SHARED lock until the
	// first write; another writer can interpose between the SHARED acquisition
	// and the RESERVED upgrade and return SQLITE_BUSY before busy_timeout
	// even gets a chance to retry (the upgrade attempt is not retried under
	// busy_timeout). IMMEDIATE eliminates this race entirely. A bounded
	// RetryOnBusy envelope around this raw transaction attempt still handles
	// DELETE-journal readers blocking COMMIT, or a competing writer already
	// holding RESERVED when this raw BEGIN IMMEDIATE starts.
	if _, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return 0, fmt.Errorf("begin immediate: %w", err)
	}
	// rollback is a no-op after a successful COMMIT; safe to call from defer.
	committed := false
	defer func() {
		if !committed {
			conn.ExecContext(context.Background(), "ROLLBACK") //nolint:errcheck
		}
	}()

	bw := newBatchWrite(ctx, w, conn, stmts, harness, resourceAttrs, signals)
	for i := range signals {
		if err = bw.writeSignal(&signals[i]); err != nil {
			return bw.inserted, err
		}
	}
	if err = bw.pruneMetrics(); err != nil {
		return bw.inserted, err
	}

	if _, err = conn.ExecContext(ctx, "COMMIT"); err != nil {
		return bw.inserted, fmt.Errorf("commit: %w", err)
	}
	committed = true
	return bw.inserted, nil
}

// batchConn resolves the connection + prepared statements for one batch.
// release returns them; it closes the per-batch statements before the
// connection, matching the original deferred order.
func (w *Writer) batchConn(ctx context.Context) (conn *sql.Conn, stmts *batchStmts, release func(), err error) {
	// Resolve the connection + prepared statements for this batch.
	//
	//   - own-pool mode: the lifetime pinned conn + lifetime statements.
	//   - shared mode (NewWriterFromDB): acquire the pool's connection for
	//     the duration of this batch and prepare statements on it, then
	//     release on completion. The shared pool is MaxOpenConns=1, so this
	//     acquire blocks until any in-flight applier/maintenance write
	//     finishes — which is exactly the single-writer serialization we
	//     want (no two BEGIN IMMEDIATE can ever overlap).
	conn = w.conn
	stmts = &batchStmts{
		insert: w.insertStmt,
		sess:   w.sessStmt,
		res:    w.resStmt,
	}
	if !w.shared {
		return conn, stmts, func() {}, nil
	}
	conn, err = w.db.Conn(ctx)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("acquire shared conn: %w", err)
	}
	stmts, err = prepareBatchStmts(ctx, conn)
	if err != nil {
		conn.Close() // release back to the pool
		return nil, nil, nil, err
	}
	return conn, stmts, func() {
		stmts.close()
		conn.Close() // release back to the pool
	}, nil
}

// batchWrite is the per-attempt state of one WriteBatch transaction.
type batchWrite struct {
	w             *Writer
	ctx           context.Context
	conn          *sql.Conn
	stmts         *batchStmts
	harness       otel.Harness
	resourceAttrs map[string]any

	seen map[string]bool
	// metricSessions tracks which sessions had at least one metric-kind
	// signal actually written in this batch (bug-e764997c): pruneMetricSignals
	// only needs to run for a session when its metric row count could have
	// grown, so a batch containing only span/log signals for a session must
	// not pay for it. Before this guard, pruneMetricSignals ran unconditionally
	// once per session per WriteBatch call regardless of signal kind -- and
	// since production calls WriteBatch once per signal (see
	// indexer.writeParsedBatch), that meant the query ran on EVERY signal
	// write, not just metric ones. Its own cost scales with the session's
	// accumulated metric row count (a DELETE with an ORDER BY/LIMIT
	// subquery, re-sorted on every call): measured at ~13ms/call even at
	// steady state (pruned back to the 5000-row keep limit), and much worse
	// before that limit is first reached. On this repo's real corpus one
	// session alone has 7,715 metric-kind signals out of 86,761 total, so
	// the vast majority of those unconditional prune calls were pure waste
	// -- this was the dominant cost behind reindexOtelEvents' real-corpus
	// 48m28s, not per-signal transaction overhead or resolution logic.
	//
	// CONCURRENCY NOTE: skipping the prune when this call adds no metric
	// signal is only a no-op for the keep-limit invariant if THIS Writer is
	// the only thing that can add metric rows for the session. That is not
	// universally true: a `wipnote reindex` invocation (own-pool Writer,
	// receiver.NewWriter) and the dashboard daemon's indexer (shared-mode
	// Writer, receiver.NewWriterFromDB) are separate Writer instances with
	// no mutex between them if both run against the same project
	// concurrently -- the same structural gap tracked in bug-fd595769 for
	// the NDJSON checkpoint. In that scenario the OTHER writer's metric
	// insert could push a session over the keep limit while THIS writer's
	// non-metric batch skips its own prune call, so the limit is enforced
	// eventually (by whichever writer next processes a metric signal for
	// that session), not instantaneously. This was already true before this
	// guard: two Writer instances each running their own unconditional
	// pruneMetricSignals call were never coordinated either, so this change
	// does not introduce a new correctness gap, only makes explicit that the
	// keep limit was already a converging target, not a hard invariant.
	metricSessions   map[string]bool
	featureCache     map[string]string
	resObservedAt    int64
	agentIDByOwnSpan map[string]string

	inserted int
}

// newBatchWrite builds the per-attempt state for signals.
func newBatchWrite(ctx context.Context, w *Writer, conn *sql.Conn, stmts *batchStmts, harness otel.Harness, resourceAttrs map[string]any, signals []otel.UnifiedSignal) *batchWrite {
	// Track sessions we've already upserted this batch so we don't
	// fire a redundant INSERT per signal.
	seen := map[string]bool{}
	// See the batchWrite.metricSessions field for why this is tracked.
	metricSessions := map[string]bool{}
	// Cache of active work item per (session_id, agent_id) pair, keyed
	// internally by resolveFeatureID. Populated lazily so a batch touching
	// many agents issues at most one SELECT per distinct pair, regardless
	// of signal count.
	featureCache := map[string]string{}
	resObservedAt := time.Now().UnixMicro()

	// agentIDByOwnSpan maps span_id -> the span's own native "agent_id"
	// attribute (feat-be696acc), built once up front from every signal in
	// this batch regardless of array order. A child span's parent is often
	// in the very same batch — sometimes later in the array than the child,
	// since OTel spans typically export on completion and children finish
	// before their parent — so resolveSignalAgentID's one-hop parent lookup
	// checks this map before falling back to an already-committed DB row.
	agentIDByOwnSpan := make(map[string]string, len(signals))
	for i := range signals {
		if signals[i].SpanID == "" {
			continue
		}
		if aid := rawAgentID(signals[i].RawAttrs); aid != "" {
			agentIDByOwnSpan[signals[i].SpanID] = aid
		}
	}
	return &batchWrite{
		w: w, ctx: ctx, conn: conn, stmts: stmts,
		harness: harness, resourceAttrs: resourceAttrs,
		seen: seen, metricSessions: metricSessions, featureCache: featureCache,
		resObservedAt:    resObservedAt,
		agentIDByOwnSpan: agentIDByOwnSpan,
	}
}

// writeSignal persists one signal inside the batch transaction.
func (bw *batchWrite) writeSignal(s *otel.UnifiedSignal) error {
	if !bw.admitSignal(s) {
		return nil
	}
	if err := bw.upsertSessionOnce(s.SessionID); err != nil {
		return err
	}
	attrsJSON := signalAttrsJSON(s)
	successVal := signalSuccess(s)

	// Resolve this signal's own agent identity, then the work item that
	// agent has claimed (feat-be696acc). subagent_invocation spans are
	// resolved here, ahead of the placeholder-upgrade branch below,
	// because the re-attribution pass further down never touches their
	// parent_span (it explicitly skips CanonicalSubagent) — so it's
	// already final and safe to resolve early. Every other canonical is
	// re-resolved after re-attribution runs (see below), since
	// re-attribution can rewrite s.ParentSpan and the one-hop lookup
	// must see the corrected value, not the pre-correction one.
	var agentID, featureID string
	resolvedEarly := s.Kind == otel.KindSpan && s.CanonicalName == otel.CanonicalSubagent
	if resolvedEarly {
		agentID, featureID = bw.resolveIdentity(s)
	}

	bw.repairParentage(s)

	// Everything except the early-resolved subagent_invocation branch
	// above resolves here, now that re-attribution has had its chance to
	// correct s.ParentSpan — see the comment on resolvedEarly.
	if !resolvedEarly {
		agentID, featureID = bw.resolveIdentity(s)
	}
	return bw.insertSignal(s, attrsJSON, successVal, featureID, agentID)
}

// admitSignal reports whether s is persisted, and marks its session as
// having a metric signal when it is one.
func (bw *batchWrite) admitSignal(s *otel.UnifiedSignal) bool {
	if s.SessionID == "" {
		// Drop signals without a session. OTel emissions always
		// carry session.id either on the resource or the signal;
		// a missing one means the adapter couldn't normalize.
		return false
	}
	if !shouldPersistSignal(s) {
		return false
	}
	if s.Kind == otel.KindMetric {
		bw.metricSessions[s.SessionID] = true
	}
	return true
}

// signalAttrsJSON marshals the signal's raw attributes, falling back to {}.
func signalAttrsJSON(s *otel.UnifiedSignal) []byte {
	attrsJSON, jerr := json.Marshal(s.RawAttrs)
	if jerr != nil {
		attrsJSON = []byte(`{}`)
	}
	return attrsJSON
}

// signalSuccess converts the optional success flag to a nullable column.
func signalSuccess(s *otel.UnifiedSignal) sql.NullInt64 {
	var successVal sql.NullInt64
	if s.Success != nil {
		successVal.Valid = true
		if *s.Success {
			successVal.Int64 = 1
		}
	}
	return successVal
}

// resolveIdentity resolves the signal's own agent identity, then the work
// item that agent has claimed (feat-be696acc).
func (bw *batchWrite) resolveIdentity(s *otel.UnifiedSignal) (agentID, featureID string) {
	agentID = resolveSignalAgentID(bw.ctx, bw.conn, s, bw.agentIDByOwnSpan)
	featureID = resolveFeatureID(bw.ctx, bw.conn, s.SessionID, agentID, bw.featureCache)
	return agentID, featureID
}

// upsertSessionOnce upserts the sessions placeholder and the resource
// attribute snapshot the first time the batch sees sessionID.
func (bw *batchWrite) upsertSessionOnce(sessionID string) error {
	if bw.seen[sessionID] {
		return nil
	}
	ctx, harness := bw.ctx, bw.harness
	agent := string(harness)
	if _, err := bw.stmts.sess.ExecContext(ctx, sessionID, agent); err != nil {
		return fmt.Errorf("sessions upsert: %w", err)
	}
	// Persist the resource attributes snapshot for this session.
	for k, v := range bw.resourceAttrs {
		if sv, ok := valueString(v); ok {
			if _, err := bw.stmts.res.ExecContext(ctx, sessionID, string(harness), k, sv, bw.resObservedAt); err != nil {
				return fmt.Errorf("resource attr upsert: %w", err)
			}
		}
	}
	bw.seen[sessionID] = true
	return nil
}

// repairParentage re-attributes mis-parented spans before INSERT. The
// orphan-span placeholder that used to run here read pending_subagent_starts,
// which no hook has written since feat-174dc958; it was removed in
// feat-ddeaa0d1.
func (bw *batchWrite) repairParentage(s *otel.UnifiedSignal) {
	ctx, conn := bw.ctx, bw.conn
	// Re-attribution: correct mis-parented spans before INSERT.
	// Some subagent-emitted spans arrive with parent_span pointing to the
	// interaction span instead of the Agent span due to a TRACEPARENT propagation
	// gap in Claude Code. We detect and fix this here so otel_signals.parent_span
	// is correct from the start (overlap-window strategy).
	if s.Kind == otel.KindSpan && s.ParentSpan != "" && s.CanonicalName != otel.CanonicalSubagent {
		if newParent, reason := tryReattributeParent(ctx, conn, s, bw.resourceAttrs); newParent != "" {
			log.Printf("reattribute: span=%s old_parent=%s new_parent=%s reason=%s",
				s.SpanID, s.ParentSpan, newParent, reason)
			s.ParentSpan = newParent
		}
	}
}

// insertSignal INSERTs the signal row and counts it when it landed.
func (bw *batchWrite) insertSignal(s *otel.UnifiedSignal, attrsJSON []byte, successVal sql.NullInt64, featureID, agentID string) error {
	res, execErr := bw.stmts.insert.ExecContext(bw.ctx,
		s.SignalID, string(s.Harness), s.SessionID, nullStr(s.PromptID),
		nullStr(s.TraceID), nullStr(s.SpanID), nullStr(s.ParentSpan),
		string(s.Kind), s.CanonicalName, s.NativeName, s.Timestamp.UnixMicro(),
		nullStr(s.ToolName), nullStr(s.ToolUseID), nullStr(s.Model),
		nullStr(s.Decision), nullStr(s.DecisionSource),
		nullInt64(s.Tokens.Input), nullInt64(s.Tokens.Output),
		nullInt64(s.Tokens.CacheRead), nullInt64(s.Tokens.CacheCreation),
		nullInt64(s.Tokens.Thought), nullInt64(s.Tokens.Tool), nullInt64(s.Tokens.Reasoning),
		nullFloat(s.CostUSD), nullStr(string(s.CostSource)),
		nullInt64(s.DurationMs), successVal, nullStr(s.ErrorMsg),
		nullInt(s.Attempt), nullInt(s.StatusCode),
		string(attrsJSON), nullStr(featureID), nullStr(agentID),
	)
	if execErr != nil {
		return fmt.Errorf("insert signal %s: %w", s.SignalID, execErr)
	}
	if n, rowsErr := res.RowsAffected(); rowsErr == nil {
		bw.inserted += int(n)
	}
	return nil
}

// pruneMetrics enforces the metric keep limit for every session that had a
// metric signal written in this batch.
func (bw *batchWrite) pruneMetrics() error {
	for sessionID := range bw.seen {
		if !bw.metricSessions[sessionID] {
			continue
		}
		if err := pruneMetricSignals(bw.ctx, bw.conn, sessionID, metricRowsPerSessionLimit); err != nil {
			return fmt.Errorf("prune metric signals for session %s: %w", sessionID, err)
		}
	}
	return nil
}

func shouldPersistSignal(s *otel.UnifiedSignal) bool {
	if s.Kind == otel.KindMetric && s.CanonicalName == otel.CanonicalUnknown {
		return false
	}
	return true
}

func pruneMetricSignals(ctx context.Context, conn dbExecer, sessionID string, keep int) error {
	if keep <= 0 {
		_, err := conn.ExecContext(ctx,
			`DELETE FROM otel_signals WHERE session_id = ? AND kind = 'metric'`,
			sessionID,
		)
		return err
	}
	_, err := conn.ExecContext(ctx, `
		DELETE FROM otel_signals
		WHERE session_id = ?
		  AND kind = 'metric'
		  AND signal_id NOT IN (
			SELECT signal_id
			FROM otel_signals
			WHERE session_id = ? AND kind = 'metric'
			ORDER BY ts_micros DESC, created_at DESC, signal_id DESC
			LIMIT ?
		  )`,
		sessionID, sessionID, keep,
	)
	return err
}
