package receiver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/observe/otel"
)

// maybeCreatePlaceholder synthesises a subagent_invocation placeholder row when
// an incoming span's parent_span does not yet exist in otel_signals. It reads
// wipnote.agent_id from resourceAttrs to look up pending_subagent_starts.
// Errors are logged at the call site and never propagate to the caller.
//
// conn accepts any dbExecer (a pinned *sql.Conn in production). Using the
// same conn that holds the BEGIN IMMEDIATE transaction avoids opening a
// second connection on the MaxOpenConns=1 pool, which would deadlock.
func (w *Writer) maybeCreatePlaceholder(
	ctx context.Context,
	conn dbExecer,
	placeholderStmt *sql.Stmt,
	s *otel.UnifiedSignal,
	resourceAttrs map[string]any,
	spanExists map[string]bool,
	resObservedAt int64,
) error {
	parentSpan := s.ParentSpan
	if !parentSpanMissing(ctx, conn, parentSpan, spanExists) {
		return nil
	}

	// Parent is missing. Check for wipnote.agent_id on the resource.
	agentID, _ := resourceAttrs["wipnote.agent_id"].(string)
	if agentID == "" {
		// No agent_id → we can't look up pending_subagent_starts. Degrade gracefully.
		return nil
	}

	pending, ok := lookupPendingSubagentStart(ctx, conn, agentID)
	if !ok {
		return nil
	}

	if err := insertPlaceholderRow(ctx, placeholderStmt, s, parentSpan, agentID, pending); err != nil {
		return err
	}

	spanExists[parentSpan] = true

	backfillAgentSpanID(ctx, conn, parentSpan, agentID)

	// Mark consumed for observability using a deferred write so we don't
	// block the transaction on an extra round-trip. MarkPendingSubagentConsumed
	// is best-effort; skip it here to avoid the w.db deadlock.
	// The consumed_at column will be set on the next periodic purge sweep.
	_ = resObservedAt

	return nil
}

// insertPlaceholderRow inserts the subagent_invocation placeholder row for the
// missing parentSpan.
func insertPlaceholderRow(ctx context.Context, placeholderStmt *sql.Stmt, s *otel.UnifiedSignal, parentSpan, agentID string, pending dbpkg.PendingSubagentStart) error {
	// Build a minimal attrs_json for the placeholder.
	placeholderAttrs := map[string]any{
		"_pending":           true,
		"agent_type":         pending.AgentType,
		"wipnote.agent_id":   agentID,
		"placeholder_source": "subagent_start_hook",
	}
	attrsJSON, _ := json.Marshal(placeholderAttrs)

	// The placeholder signal_id is deterministic so re-delivery of the same
	// first orphan span doesn't create duplicate placeholders.
	placeholderSignalID := "placeholder:" + parentSpan

	if _, err := placeholderStmt.ExecContext(ctx,
		placeholderSignalID, string(s.Harness), pending.SessionID,
		nullStr(s.TraceID), parentSpan,
		pending.CreatedAt, string(attrsJSON),
	); err != nil {
		return fmt.Errorf("insert placeholder: %w", err)
	}
	return nil
}

// backfillAgentSpanID back-fills the agent_span_id mapping so Strategy A
// re-attribution can resolve (session_id, agent_id) → agent span_id without
// scanning otel_signals. We use the same conn to avoid the w.db deadlock on
// the single connection. Best-effort: ignore errors since re-attribution
// degrades gracefully.
func backfillAgentSpanID(ctx context.Context, conn dbExecer, parentSpan, agentID string) {
	if _, err := conn.ExecContext(ctx,
		`UPDATE pending_subagent_starts SET agent_span_id = ? WHERE agent_id = ?`,
		parentSpan, agentID,
	); err != nil {
		// Non-fatal: re-attribution will fall back to Strategy B.
		_ = err
	}
}

// parentSpanMissing reports whether parentSpan is provably absent from
// otel_signals, checking the batch cache first, then the DB. A failed lookup
// is non-fatal and answers false.
func parentSpanMissing(ctx context.Context, conn dbExecer, parentSpan string, spanExists map[string]bool) bool {
	// Check cache first, then DB.
	if exists, ok := spanExists[parentSpan]; ok && exists {
		return false
	}

	var n int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM otel_signals WHERE span_id = ?`, parentSpan,
	).Scan(&n); err != nil {
		return false // non-fatal
	}
	spanExists[parentSpan] = n > 0
	// n > 0: parent already exists; nothing to do.
	return n == 0
}

// lookupPendingSubagentStart looks up the pending row using the live conn
// (MaxOpenConns=1 — we MUST use conn, not w.db, to avoid a deadlock on the
// single connection). ok is false when there is no pending row (subagent
// started before this feature shipped) or the lookup failed (non-fatal).
func lookupPendingSubagentStart(ctx context.Context, conn dbExecer, agentID string) (dbpkg.PendingSubagentStart, bool) {
	var pending dbpkg.PendingSubagentStart
	var cwd sql.NullString
	err := conn.QueryRowContext(ctx, `
		SELECT agent_id, agent_type, session_id, cwd, created_at
		FROM pending_subagent_starts
		WHERE agent_id = ?`, agentID,
	).Scan(&pending.AgentID, &pending.AgentType, &pending.SessionID, &cwd, &pending.CreatedAt)
	if err != nil {
		return pending, false
	}
	pending.CWD = cwd.String
	return pending, true
}

// tryUpgradePlaceholder upgrades a placeholder subagent_invocation row with
// real Agent span data. Returns true if a placeholder was found and upgraded.
// Returns false when no placeholder exists (caller should proceed with normal INSERT).
func tryUpgradePlaceholder(
	ctx context.Context,
	upgradeStmt *sql.Stmt,
	s *otel.UnifiedSignal,
	attrsJSON []byte,
	successVal sql.NullInt64,
	featureID string,
	agentID string,
) (bool, error) {
	res, err := upgradeStmt.ExecContext(ctx,
		s.SignalID, string(s.Harness),
		nullStr(s.PromptID),
		nullStr(s.TraceID),
		nullStr(s.ParentSpan),
		s.NativeName,
		s.Timestamp.UnixMicro(),
		nullStr(s.ToolName), nullStr(s.ToolUseID), nullStr(s.Model),
		nullStr(s.Decision), nullStr(s.DecisionSource),
		nullInt64(s.Tokens.Input), nullInt64(s.Tokens.Output),
		nullInt64(s.Tokens.CacheRead), nullInt64(s.Tokens.CacheCreation),
		nullInt64(s.Tokens.Thought), nullInt64(s.Tokens.Tool), nullInt64(s.Tokens.Reasoning),
		nullFloat(s.CostUSD), nullStr(string(s.CostSource)),
		nullInt64(s.DurationMs), successVal, nullStr(s.ErrorMsg),
		nullInt(s.Attempt), nullInt(s.StatusCode),
		string(attrsJSON), nullStr(featureID), nullStr(agentID),
		s.SpanID, // WHERE span_id = ?
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
