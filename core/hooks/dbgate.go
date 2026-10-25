// Package hooks — dbgate.go: the daemon-only derived-write gate for hook handlers.
//
// Hooks write canonical files (NDJSON, HTML, ledgers) first. Derived-index
// writes are then offered to the per-project writer daemon through
// RouteHookWrite / RouteInsertEvent (enqueue-only, apply.RouteSQLAsync) or,
// for the two applied-ack writes, routeSQLApplied (apply.RouteSQL). The daemon
// applies them to its own in-memory projection.
//
// There is no daemon-miss fallback (feat-174dc958): when no daemon answers,
// the write is dropped and nothing is written to the per-project file
// database. OpenHookDB / OpenHookDBWithBusyTimeout below remain only because
// cmd/wipnote/hook.go still opens a handle for the cold and session hooks; do
// NOT add new db.Open call sites in core/hooks/ or cmd/wipnote/hook.go.
package hooks

import (
	"database/sql"
	"sync/atomic"
	"time"

	"github.com/shakestzd/wipnote/core/daemon/apply"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// FallbackReason is the structured label emitted when a hook's derived-index
// write path cannot proceed and the handler falls back to canonical-only
// persistence. The labels match the contract from the plan critique:
// "writer_unavailable", "queue_full", "timeout".
type FallbackReason string

const (
	// FallbackWriterUnavailable means the writable SQLite handle could not
	// be opened (or a queue was supplied but is stopped/never-started).
	FallbackWriterUnavailable FallbackReason = "writer_unavailable"
	// FallbackQueueFull means the in-process writer queue was at capacity
	// when the hook tried to submit a derived-index op. Only emitted when
	// a queue is wired in (in-process hook callers); subprocess hooks
	// always use FallbackWriterUnavailable for open failures.
	FallbackQueueFull FallbackReason = "queue_full"
	// FallbackTimeout means SubmitWithTimeout's deadline elapsed before a
	// slot opened. Only emitted by queue-backed callers.
	FallbackTimeout FallbackReason = "timeout"
)

// Fallback counters — process-level metrics so the dashboard /api/collector-status
// surface (slice-10 will extend this) can show how often hooks degraded to
// canonical-only mode. Atomic so they remain safe for concurrent hook goroutines
// in the in-process runner.
var (
	fallbackWriterUnavailable atomic.Int64
	fallbackQueueFull         atomic.Int64
	fallbackTimeout           atomic.Int64
)

// RecordFallback bumps the appropriate counter and emits a structured log
// line tagged with the reason. handler/sessionID let the log line correlate
// with the rest of the hook trace.
func RecordFallback(handler, sessionID string, reason FallbackReason, detail string) {
	switch reason {
	case FallbackWriterUnavailable:
		fallbackWriterUnavailable.Add(1)
	case FallbackQueueFull:
		fallbackQueueFull.Add(1)
	case FallbackTimeout:
		fallbackTimeout.Add(1)
	}
	if !debugLogEnabled() {
		return
	}
	projectDir := resolveLogDir()
	if projectDir == "" {
		return
	}
	fields := map[string]string{"fallback": string(reason)}
	if sessionID != "" {
		fields["session"] = sessionID[:minSessionLen(sessionID)]
	}
	if detail != "" {
		fields["detail"] = detail
	}
	debugLogFields(projectDir, handler, fields, "canonical-first fallback engaged")
}

// OpenHookDB returns a writable DB handle for the hook subprocess to use as
// the daemon-miss fallback for derived-index updates. On open failure it
// returns (nil, reason). The reason is logged + counted; callers MUST treat
// a nil DB as a signal to skip DB-dependent work and return success.
//
// This open is the DAEMON-MISS FALLBACK ONLY (plan-2390966a slices 3-7):
// the hot hook write path (RouteHookWrite / RouteInsertEvent) reaches here
// only when apply.RouteSQLAsync returns false (daemon unreachable / spawn-
// forbidden / queue full). On a reachable-daemon system this open is rarely
// or never called. The canonical NDJSON write upstream is always
// authoritative; reindex recovers any rows neither path could write.
//
// This is the ONLY allowed direct writable open in the hook tree
// (class canonicalFirstHookFallback). Do not add new db.Open call sites in
// core/hooks/ or cmd/wipnote/hook.go.
func OpenHookDB(handler, sessionID, dbPath string) (*sql.DB, FallbackReason) {
	database, err := db.Open(dbPath)
	if err != nil {
		// Slice-10 contention observability: classify open failures by
		// hook_writer subsystem so the launch gate can assert zero BUSY
		// from the hook tree. Non-BUSY open failures (e.g., schema lock,
		// disk full) bypass the counter; the structured fallback log
		// upstream captures those.
		db.Record(db.SubsystemHookWriter, err)
		RecordFallback(handler, sessionID, FallbackWriterUnavailable, err.Error())
		return nil, FallbackWriterUnavailable
	}
	return database, ""
}

// SessionStartBusyTimeout bounds how long the session-start hook's writable DB
// handle waits on a held write lock before failing fast. The session-start hook
// runs on the launcher's POST-selection critical path: Claude blocks on the
// hook's additionalContext, so a write that stalls for the default 5s
// busy_timeout (under contention from a freshly-spawned `_serve-child`
// writer daemon / per-session `otel-collect`) directly delays the interactive
// session by that long (bug-504095f2).
//
// 750ms is the chosen bound: long enough to ride out the brief lock overlap a
// healthy single writer produces, short enough that a genuinely contended lock
// degrades to canonical-only persistence (logged + counted) in well under a
// second instead of stalling ~5s. Every session-start derived write is
// best-effort — failures are swallowed via the canonical-first fallback and
// recovered by reindex from canonical NDJSON — so failing fast here only skips a
// derived-index write, never user data.
const SessionStartBusyTimeout = 750 * time.Millisecond

// OpenHookDBWithBusyTimeout is OpenHookDB with a caller-chosen busy_timeout on
// the returned writable handle, so contention-prone writes fail fast instead of
// stalling for the connection-default 5s. It exists for the session-start hook
// path (see SessionStartBusyTimeout); other hooks keep OpenHookDB's default
// timeout. busyTimeout <= 0 is identical to OpenHookDB.
//
// Failure semantics are unchanged from OpenHookDB: a failed open is classified
// under hook_writer, logged + counted as writer_unavailable, and returns a nil
// handle the caller MUST treat as a signal to return canonical-success.
//
// The writable daemon-miss fallback MUST open the canonical SQLite DB, because
// callers expect the fallback write to be visible to the same database handle
// they already hold.
func OpenHookDBWithBusyTimeout(handler, sessionID, dbPath string, busyTimeout time.Duration) (*sql.DB, FallbackReason) {
	database, err := db.OpenWithBusyTimeout(dbPath, busyTimeout)
	if err != nil {
		db.Record(db.SubsystemHookWriter, err)
		RecordFallback(handler, sessionID, FallbackWriterUnavailable, err.Error())
		return nil, FallbackWriterUnavailable
	}
	return database, ""
}

// routeSQLAsync is the daemon enqueue-only seam used by RouteHookWrite. It is a
// package-level var (not a direct call) ONLY so tests can stub the daemon hop —
// production always binds it to apply.RouteSQLAsync. Mirrors the indirection the
// daemon-routing tests rely on elsewhere; do not call it directly from other
// hook code (use apply.RouteSQLAsync) so the override stays test-local.
var routeSQLAsync = apply.RouteSQLAsync

// routeSQLApplied is the daemon APPLIED-ack seam (synchronous: returns true only
// once the writer has committed the statement). Like routeSQLAsync it is a
// package-level var ONLY so tests can stub the daemon hop — production always
// binds it to apply.RouteSQL. It is used by the few LOW-FREQUENCY hot-hook writes
// whose consumer is coupled to them and therefore require synchronous visibility
// (roborev-473): subagent-start's pending_subagent_starts upsert (the OTLP
// receiver reads it the instant the first span arrives) and session-start's
// session_family_id update (routeFamilyAttribution reads the family the instant
// after). Do not call it directly from other hook code (use apply.RouteSQL) so
// the override stays test-local.
var routeSQLApplied = apply.RouteSQL

// RouteHookWrite is the single primitive every hook derived-index write goes
// through. It offers the parameterized statement to the writer daemon,
// enqueue-only (routeSQLAsync), and returns whether the daemon took it.
//
// There is NO fallback (feat-174dc958). When the daemon is unreachable, its
// queue is full, or the project has none, the write is dropped: nothing is
// written to the per-project file database in its place. No reader of that
// file needed what the fallback wrote, and the daemon itself keeps its writes
// in its own in-memory projection. A miss is counted and, under WIPNOTE_DEBUG,
// logged as writer_unavailable.
//
// The call never blocks beyond apply.AsyncEnqueueBudget, and with no socket it
// returns at once. The return value is advisory: callers MUST NOT propagate a
// miss to the hook protocol. args are bound as SQL parameters and MUST NOT be
// interpolated into sql by the caller.
func RouteHookWrite(handler, projectRoot, sessionID, sql string, args ...any) bool {
	if routeSQLAsync(projectRoot, sql, args...) {
		return true
	}
	RecordFallback(handler, sessionID, FallbackWriterUnavailable, "daemon miss: write dropped")
	return false
}

// agentEventInsertSQL is the parameterized INSERT for an agent_events row. It is
// the EXACT statement db.InsertEvent issues (core/db/event_repo.go) — kept
// byte-identical here so the hot hooks (pretooluse tool_call insert, Stop
// terminal event) can route the same write through RouteHookWrite's daemon-first
// enqueue path instead of opening a direct writable handle. Column order and the
// 22 placeholders MUST stay in lock-step with db.InsertEvent.
const agentEventInsertSQL = `
	INSERT INTO agent_events (
		event_id, agent_id, event_type, timestamp, tool_name,
		input_summary, tool_input, output_summary, session_id, feature_id,
		parent_agent_id, parent_event_id, subagent_type,
		cost_tokens, execution_duration_seconds, status,
		model, claude_task_id, source, step_id,
		created_at, updated_at
	) VALUES (?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?,?,?,?, ?,?)`

// nullableArg maps an empty string to a typed SQL NULL (nil) and any non-empty
// string to itself. Unlike sql.NullString it is JSON-transport-safe: the daemon
// enqueue path JSON-encodes RouteHookWrite's args (core/daemon/apply.DerivedOp),
// and a plain string / nil binds identically on BOTH the daemon's ExecContext
// and the direct-fallback Exec — whereas a sql.NullString would JSON-marshal to
// an object the SQLite driver cannot bind. This reproduces db.nullStr's NULL
// semantics across the process boundary (parity with db.InsertEvent's columns).
func nullableArg(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// agentEventInsertArgs renders an AgentEvent into the 22 bind parameters for
// agentEventInsertSQL, matching db.InsertEvent's argument order and NULL
// handling exactly. Timestamps are RFC3339 strings; absent text columns become
// typed NULLs via nullableArg so the row is indistinguishable from one written
// by the direct db.InsertEvent path.
func agentEventInsertArgs(e *models.AgentEvent) []any {
	return []any{
		e.EventID, e.AgentID, string(e.EventType),
		e.Timestamp.UTC().Format(time.RFC3339), nullableArg(e.ToolName),
		nullableArg(e.InputSummary), nullableArg(e.ToolInput), nullableArg(e.OutputSummary),
		e.SessionID, nullableArg(e.FeatureID),
		nullableArg(e.ParentAgentID), nullableArg(e.ParentEventID),
		nullableArg(e.SubagentType),
		e.CostTokens, e.ExecDuration, e.Status,
		nullableArg(e.Model), nullableArg(e.ClaudeTaskID),
		e.Source, nullableArg(e.StepID),
		e.CreatedAt.UTC().Format(time.RFC3339),
		e.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

// RouteInsertEvent routes an agent_events INSERT through RouteHookWrite's
// daemon-only policy — the hot-hook equivalent of
// db.InsertEvent. It mirrors db.InsertEvent's one pre-write read: when
// ParentEventID is set but ParentAgentID is empty, the parent row's agent_id is
// resolved from `database` first so the lineage edge is materialised at insert
// time. That lookup is a READ (it never contends the write lock that stalled the
// hot hooks) and is skipped when `database` is nil (daemon-only callers).
//
// Like every RouteHookWrite caller this is best-effort: a daemon miss drops the
// row. The return value is advisory; callers MUST NOT propagate it to the hook
// protocol.
func RouteInsertEvent(handler, projectRoot, sessionID string, ev *models.AgentEvent, database *sql.DB) bool {
	if ev == nil {
		return true
	}
	// Materialise the parent_agent_id lineage edge exactly as db.InsertEvent
	// does — a pure read, safe to run direct even under write contention.
	if ev.ParentEventID != "" && ev.ParentAgentID == "" && database != nil {
		var parentAgentID string
		if err := database.QueryRow(
			`SELECT agent_id FROM agent_events WHERE event_id = ?`, ev.ParentEventID,
		).Scan(&parentAgentID); err == nil {
			ev.ParentAgentID = parentAgentID
		}
	}
	return RouteHookWrite(handler, projectRoot, sessionID, agentEventInsertSQL, agentEventInsertArgs(ev)...)
}
