package receiver

import (
	"context"
	"database/sql"
	"log"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/observe/otel"
)

// tryReattributeParent checks whether the incoming span is mis-parented (i.e.,
// its parent_span points to an interaction span instead of the enclosing Agent
// span): when the span's timestamp falls within exactly one sibling
// subagent_invocation span's [ts, ts+duration] window and the current parent
// is an interaction canonical, it is re-parented to that span.
//
// A span carrying the wipnote.agent_id resource attribute is left as is. Its
// mapping (Strategy A) read agent_span_id from pending_subagent_starts, which
// no hook has written since feat-174dc958, so it always fell through to no
// change; the reader was removed in feat-ddeaa0d1 and the outcome is kept.
//
// Returns (newParentSpanID, reason) when re-attribution applies, or ("", "")
// when no fix is warranted.
func tryReattributeParent(
	ctx context.Context,
	conn dbExecer,
	s *otel.UnifiedSignal,
	resourceAttrs map[string]any,
) (newParent, reason string) {
	if agentID, _ := resourceAttrs["wipnote.agent_id"].(string); agentID != "" {
		return "", ""
	}

	// Strategy B: overlap window fallback for pre-feat-e1efb972 sessions.
	// Only applies when the current parent is an interaction span.
	if s.SessionID == "" || s.ParentSpan == "" {
		return "", ""
	}

	// Check that the current parent is indeed an interaction canonical.
	//
	// span_id is not unique (bug-0fc17d53): the row that IS the span shares
	// its id with every log record and metric correlated to it. Prefer the
	// span row and break ties deterministically, so this lookup answers "what
	// is that span" rather than "whichever row for that span the storage
	// engine happened to return first".
	var parentCanonical sql.NullString
	if err := conn.QueryRowContext(ctx,
		`SELECT canonical FROM otel_signals WHERE span_id = ?
		 ORDER BY (kind = 'span') DESC, ts_micros ASC, signal_id ASC
		 LIMIT 1`, s.ParentSpan,
	).Scan(&parentCanonical); err != nil || parentCanonical.String != otel.CanonicalInteraction {
		return "", ""
	}

	matches, ok := overlappingAgentSpans(ctx, conn, s)
	if !ok {
		return "", ""
	}

	if len(matches) == 1 {
		if matches[0] != s.ParentSpan {
			return matches[0], "strategy_b_overlap_window"
		}
	} else if len(matches) > 1 {
		// Ambiguous: multiple overlapping Agent spans — skip re-parenting.
		log.Printf("reattribute: span=%s ambiguous overlap (%d agent windows), skipping", s.SpanID, len(matches))
	}

	return "", ""
}

// overlappingAgentSpans returns the span_ids of the subagent_invocation spans
// in s's session whose [start, start+duration] window contains s. ok is false
// when the query fails.
func overlappingAgentSpans(ctx context.Context, conn dbExecer, s *otel.UnifiedSignal) (matches []string, ok bool) {
	// Find all subagent_invocation spans in this session that have a known duration.
	spanTsMicros := s.Timestamp.UnixMicro()
	rows, err := conn.QueryContext(ctx, `
		SELECT span_id, ts_micros, duration_ms
		FROM otel_signals
		WHERE session_id = ? AND canonical = ? AND duration_ms IS NOT NULL AND duration_ms > 0`,
		s.SessionID, otel.CanonicalSubagent,
	)
	if err != nil {
		return nil, false
	}
	defer rows.Close()

	for rows.Next() {
		var spanID string
		var tsMicros, durationMs int64
		if err := rows.Scan(&spanID, &tsMicros, &durationMs); err != nil {
			continue
		}
		endMicros := tsMicros + durationMs*1000 // duration_ms → microseconds
		if spanTsMicros >= tsMicros && spanTsMicros <= endMicros {
			matches = append(matches, spanID)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, false
	}
	return matches, true
}

// rawAgentID reads Claude Code's native "agent_id" span attribute straight
// off a signal's raw attributes (feat-be696acc). This is emitted directly by
// the harness's OTel SDK on claude_code.llm_request and claude_code.tool
// spans whenever the span belongs to a subagent or teammate — see
// observe/otel/adapter/claude.go, which copies OTLP attributes through
// unmodified. Distinct from the wipnote.agent_id RESOURCE attribute used
// elsewhere in this file for placeholder/re-attribution lookups: that one is
// wipnote's own hex identity propagated via CLAUDE_ENV_FILE (currently
// broken for Agent-Teams-style sessions — bug-190950e0); this one is
// harness-native, per-span, and does not depend on that propagation path.
func rawAgentID(attrs map[string]any) string {
	if attrs == nil {
		return ""
	}
	v, _ := attrs["agent_id"].(string)
	return v
}

// resolveSignalAgentID returns the agent that emitted s: its own native
// agent_id (rawAgentID) when present, else its immediate parent span's OWN
// native agent_id, else "" (root — no agent claimed this signal or its
// parent). Exactly one hop, and strictly one: the parent lookup reads the
// parent's raw attrs_json (its own native attribute), NEVER the parent's
// resolved/stored otel_signals.agent_id column — that column can itself hold
// a value the parent inherited from ITS parent, and reading it here would
// let inheritance cascade transitively across arbitrarily many generations,
// which is exactly the deep-tree walk this design deliberately avoids (see
// the primary-hypothesis research on feat-be696acc: parent chains are not
// reliably intact across older/other sessions). A child two levels below the
// nearest agent_id-bearing span is therefore correctly left unattributed
// (root), not silently inherited.
//
// The parent lookup checks agentIDByOwnSpan (this batch) before querying
// otel_signals, since a child's parent is frequently in the same batch —
// sometimes later in the array than the child, because spans typically
// export on completion and children finish before their enclosing parent.
func resolveSignalAgentID(ctx context.Context, conn dbExecer, s *otel.UnifiedSignal, agentIDByOwnSpan map[string]string) string {
	if aid := rawAgentID(s.RawAttrs); aid != "" {
		return aid
	}
	if s.ParentSpan == "" {
		return ""
	}
	if aid, ok := agentIDByOwnSpan[s.ParentSpan]; ok {
		return aid
	}
	// span_id is not unique (bug-0fc17d53), so restrict to rows that actually
	// carry an agent_id and prefer the span row itself. This mirrors what
	// agentIDByOwnSpan does for the in-batch case — any signal on that span
	// that reports an agent_id answers the question — while staying
	// deterministic when several do.
	var v sql.NullString
	_ = conn.QueryRowContext(ctx,
		`SELECT json_extract(attrs_json, '$.agent_id') FROM otel_signals
		 WHERE span_id = ? AND json_extract(attrs_json, '$.agent_id') IS NOT NULL
		 ORDER BY (kind = 'span') DESC, ts_micros ASC, signal_id ASC
		 LIMIT 1`,
		s.ParentSpan,
	).Scan(&v)
	return v.String
}

// resolveFeatureID looks up the work item claimed by (sessionID, agentID) in
// active_work_items, falling back to the session's __root__ claim when the
// resolved agent has no claim of its own. That fallback is deliberate and
// currently load-bearing: until bug-190950e0 fixes WIPNOTE_AGENT_ID
// propagation for Agent-Teams-style sessions, every concurrent agent's own
// claim collapses to __root__ same as everyone else's, so this fallback
// reproduces today's (imperfect) behavior unchanged rather than silently
// dropping feature_id to NULL. Once that propagation gap is fixed and agents
// hold real per-agent claims, this same fallback still applies correctly for
// agents that genuinely never claimed a work item of their own.
//
// cache is keyed per (sessionID, agentID) so a batch spanning many
// concurrently active agents issues at most one SELECT per distinct pair.
func resolveFeatureID(ctx context.Context, conn dbExecer, sessionID, agentID string, cache map[string]string) string {
	lookupID := dbpkg.NormaliseAgentID(agentID)
	key := sessionID + "\x00" + lookupID
	if v, ok := cache[key]; ok {
		return v
	}
	var fid sql.NullString
	_ = conn.QueryRowContext(ctx,
		`SELECT work_item_id FROM active_work_items WHERE session_id = ? AND agent_id = ?`,
		sessionID, lookupID,
	).Scan(&fid)
	result := fid.String
	if result == "" && lookupID != dbpkg.AgentRootSentinel {
		result = resolveFeatureID(ctx, conn, sessionID, dbpkg.AgentRootSentinel, cache)
	}
	cache[key] = result
	return result
}
