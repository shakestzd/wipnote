package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"
)

// turnStats holds per-turn aggregate counts.
type turnStats struct {
	ToolCount  int      `json:"tool_count"`
	ErrorCount int      `json:"error_count"`
	Models     []string `json:"models"`
}

// turn groups a UserQuery with its child events and stats.
type turn struct {
	SessionID string           `json:"session_id"`
	UserQuery map[string]any   `json:"user_query"`
	Children  []map[string]any `json:"children"`
	Stats     turnStats        `json:"stats"`
}

type otelPromptAnchor struct {
	SignalID    string
	Harness     string
	SessionID   string
	TraceID     string
	PromptID    string
	FeatureID   string
	TSMicros    int64
	RawTSMicros int64
	AttrsRaw    string
	PromptText  string
	FeatureName string
}

// hookOtelDedupWindowMicros is the timestamp window used to consider a
// hook UserQuery row "anchored" by an OTel interaction span when the hook
// row has no step_id to match exactly. Five seconds is generous compared
// to the actual sub-second offset between matching events.
const hookOtelDedupWindowMicros int64 = 5_000_000

// eventColumns is the shared SELECT column list for agent_events (aliased as e).
const eventColumns = `e.event_id,
	COALESCE(NULLIF(e.agent_id, ''), (SELECT s.agent_assigned FROM sessions s WHERE s.session_id = e.session_id LIMIT 1), ''),
	e.event_type, e.timestamp, e.tool_name,
	COALESCE(e.input_summary, ''), COALESCE(e.output_summary, ''),
	e.session_id, COALESCE(e.feature_id, ''), e.status,
	COALESCE(e.parent_event_id, ''), COALESCE(e.subagent_type, ''),
	COALESCE(e.model, ''), COALESCE(e.step_id, ''),
	COALESCE((SELECT f.title FROM features f WHERE f.id = e.feature_id LIMIT 1), '')`

// treeHandler returns hierarchical event data grouped by UserQuery turns.
// GET /api/events/tree?limit=50
func treeHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
				limit = n
			}
		}

		turns, err := buildEventTree(database, limit)
		if err != nil {
			http.Error(w, fmt.Sprintf("query event tree: %v", err), http.StatusInternalServerError)
			return
		}
		respondJSON(w, turns)
	}
}

// buildEventTree merges OTel interaction-span turns with hook-based UserQuery
// turns that have no OTel anchor, then returns the most recent `limit` by
// timestamp. Mixed databases (sessions partially anchored in OTel, older
// sessions purely hook-driven) are rendered together rather than the
// older sessions being silently dropped once any OTel span is present.
func buildEventTree(database *sql.DB, limit int) ([]turn, error) {
	otelTurns, err := buildEventTreeOtel(database, limit)
	if err != nil {
		return nil, err
	}
	otelLogTurns, err := buildEventTreeOtelLogFallback(database, limit)
	if err != nil {
		return nil, err
	}
	hookTurns, err := buildEventTreeHookUnanchored(database, limit)
	if err != nil {
		return nil, err
	}

	merged := append(otelTurns, otelLogTurns...)
	merged = append(merged, hookTurns...)
	if len(merged) == 0 {
		return []turn{}, nil
	}

	// Sort DESC by timestamp (RFC3339 strings are lexicographically sortable
	// in ascending chronological order; reverse for newest-first).
	sort.SliceStable(merged, func(i, j int) bool {
		ti, _ := merged[i].UserQuery["timestamp"].(string)
		tj, _ := merged[j].UserQuery["timestamp"].(string)
		return ti > tj
	})

	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// buildEventTreeOtel builds the turn list using OTel interaction spans as
// anchors. Each interaction span corresponds to one user prompt turn; its
// trace_id links all child spans. The user_query shape is synthesized to
// match the fields the frontend event-tree.js expects so no frontend
// changes are required.
func buildEventTreeOtel(database *sql.DB, limit int) ([]turn, error) {
	rows, err := database.Query(`
		SELECT s.signal_id, s.trace_id, COALESCE(s.span_id, ''),
		       s.session_id,
		       COALESCE(NULLIF(s.harness, ''), (SELECT sess.agent_assigned FROM sessions sess WHERE sess.session_id = s.session_id LIMIT 1), ''),
		       s.ts_micros, COALESCE(s.duration_ms, 0),
		       COALESCE(s.attrs_json, '{}'),
		       COALESCE(s.feature_id, '')
		FROM otel_signals s
		WHERE s.kind = 'span' AND s.canonical = 'interaction'
		ORDER BY s.ts_micros DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []turn
	for rows.Next() {
		var signalID, traceID, spanID, sessionID, harness string
		var tsMicros, durationMs int64
		var attrsRaw, featureID string

		if err := rows.Scan(&signalID, &traceID, &spanID, &sessionID,
			&harness, &tsMicros, &durationMs, &attrsRaw, &featureID); err != nil {
			continue
		}

		// Convert ts_micros to RFC3339 for the frontend.
		ts := time.UnixMicro(tsMicros).UTC().Format(time.RFC3339)

		// Extract prompt text from attrs_json (attrs.user_prompt, attrs.prompt).
		promptText := extractPromptText(attrsRaw)

		// If attrs_json didn't carry the prompt, look for a user_prompt log
		// record in the same trace.
		if promptText == "" && traceID != "" {
			promptText = fetchPromptFromTrace(database, traceID)
		}

		// Look up feature title when we have a feature_id.
		featureTitle := ""
		if featureID != "" {
			_ = database.QueryRow(
				`SELECT COALESCE(title, '') FROM features WHERE id = ? LIMIT 1`,
				featureID,
			).Scan(&featureTitle)
		}

		// Build the user_query map with the same shape the frontend expects.
		userQuery := map[string]any{
			"event_id":      signalID,
			"timestamp":     ts,
			"tool_name":     "UserQuery",
			"input_summary": promptText,
			"session_id":    sessionID,
			"feature_id":    featureID,
			"feature_title": featureTitle,
			// Fields not available from OTel interaction spans; set to
			// zero-values so the frontend can still render gracefully.
			"agent_id":        harness,
			"event_type":      "tool_call",
			"output_summary":  "",
			"status":          "recorded",
			"parent_event_id": "",
			"subagent_type":   "",
			"tool_use_id":     "",
			"model":           "",
		}

		// Fetch hook-based children: the frontend already renders OTel spans
		// independently via /api/otel/spans; hook children provide the
		// fallback tree for sessions that have both data sources.
		children := fetchChildrenForOtelTurn(database, traceID, sessionID)
		stats := computeOtelStats(database, traceID)

		turns = append(turns, turn{
			SessionID: sessionID,
			UserQuery: userQuery,
			Children:  children,
			Stats:     stats,
		})
	}

	return turns, nil
}

// buildEventTreeOtelLogFallback synthesizes turn rows from OTel user_prompt logs
// when no interaction span anchors the prompt. This covers Codex sessions that
// currently emit prompt/tool activity as logs only.
func buildEventTreeOtelLogFallback(database *sql.DB, limit int) ([]turn, error) {
	rows, err := database.Query(`
		SELECT s.signal_id, COALESCE(s.harness, ''), COALESCE(s.session_id, ''), COALESCE(s.trace_id, ''),
		       COALESCE(s.prompt_id, ''), COALESCE(s.feature_id, ''),
		       s.ts_micros, COALESCE(s.attrs_json, '{}'),
		       COALESCE((SELECT f.title FROM features f WHERE f.id = s.feature_id LIMIT 1), '')
		FROM otel_signals s
		WHERE s.kind = 'log' AND s.canonical = 'user_prompt'
		  AND NOT EXISTS (
		    SELECT 1 FROM otel_signals i
		    WHERE i.kind = 'span' AND i.canonical = 'interaction'
		      AND i.session_id = s.session_id
		      AND ABS(i.ts_micros - s.ts_micros) < ?
		  )`, hookOtelDedupWindowMicros)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var anchors []otelPromptAnchor
	for rows.Next() {
		var anchor otelPromptAnchor
		var attrsRaw string
		if err := rows.Scan(
			&anchor.SignalID, &anchor.Harness, &anchor.SessionID, &anchor.TraceID,
			&anchor.PromptID, &anchor.FeatureID, &anchor.RawTSMicros,
			&attrsRaw, &anchor.FeatureName,
		); err != nil {
			continue
		}
		anchor.AttrsRaw = attrsRaw
		anchor.PromptText = extractPromptText(attrsRaw)
		_, anchor.TSMicros = treeTimestampFromOtel(anchor.RawTSMicros, anchor.AttrsRaw)
		if anchor.TSMicros == 0 {
			continue
		}
		anchors = append(anchors, anchor)
	}

	if len(anchors) == 0 {
		return []turn{}, nil
	}

	sort.SliceStable(anchors, func(i, j int) bool {
		return anchors[i].TSMicros > anchors[j].TSMicros
	})
	if len(anchors) > limit {
		anchors = anchors[:limit]
	}

	sessionAnchors := make(map[string][]otelPromptAnchor)
	for _, anchor := range anchors {
		sessionAnchors[anchor.SessionID] = append(sessionAnchors[anchor.SessionID], anchor)
	}
	for sessionID := range sessionAnchors {
		sort.Slice(sessionAnchors[sessionID], func(i, j int) bool {
			return sessionAnchors[sessionID][i].TSMicros < sessionAnchors[sessionID][j].TSMicros
		})
	}

	turns := make([]turn, 0, len(anchors))
	for _, anchor := range anchors {
		windowEnd := nextPromptBoundary(sessionAnchors[anchor.SessionID], anchor.SignalID)
		children := fetchOtelLogChildren(database, anchor, windowEnd)
		stats := computeStats(children)
		timestamp, _ := treeTimestampFromOtel(anchor.RawTSMicros, anchor.AttrsRaw)

		turns = append(turns, turn{
			SessionID: anchor.SessionID,
			UserQuery: map[string]any{
				"event_id":        anchor.SignalID,
				"agent_id":        anchor.Harness,
				"event_type":      "tool_call",
				"timestamp":       timestamp,
				"tool_name":       "UserQuery",
				"input_summary":   anchor.PromptText,
				"output_summary":  "",
				"session_id":      anchor.SessionID,
				"feature_id":      anchor.FeatureID,
				"feature_title":   anchor.FeatureName,
				"status":          "recorded",
				"parent_event_id": "",
				"subagent_type":   "",
				"tool_use_id":     anchor.PromptID,
				"model":           "",
			},
			Children: children,
			Stats:    stats,
		})
	}

	return turns, nil
}

func treeTimestampFromOtel(tsMicros int64, attrsRaw string) (string, int64) {
	if tsMicros > 0 {
		return time.UnixMicro(tsMicros).UTC().Format(time.RFC3339), tsMicros
	}

	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsRaw), &attrs); err == nil {
		if raw, ok := attrs["event.timestamp"]; ok {
			if parsed, ok := parseTreeEventTimestamp(raw); ok {
				return parsed.UTC().Format(time.RFC3339), parsed.UnixMicro()
			}
		}
	}

	return time.UnixMicro(tsMicros).UTC().Format(time.RFC3339), tsMicros
}

func parseTreeEventTimestamp(v any) (time.Time, bool) {
	switch x := v.(type) {
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t.UTC(), true
		}
		if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", x); err == nil {
			return t.UTC(), true
		}
	case float64:
		return time.UnixMicro(int64(x)), true
	case int64:
		return time.UnixMicro(x), true
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return time.UnixMicro(n), true
		}
	}
	return time.Time{}, false
}

// buildEventTreeHookUnanchored returns hook-based UserQuery turns that do
// NOT correspond to an OTel interaction span. Anchoring is detected with
// two complementary checks so deduplication holds even when hook events
// arrive without a step_id:
//
//  1. step_id match — when the hook UserQuery carries the OTel trace_id,
//     skip if any interaction span shares it.
//  2. session+timestamp window — when step_id is empty, skip if any
//     interaction span exists in the same session within
//     hookOtelDedupWindowMicros of the hook event's timestamp.
//
// Buildings before OTel emission and pure hook-only sessions still pass
// both checks and remain visible in /api/events/tree.
func buildEventTreeHookUnanchored(database *sql.DB, limit int) ([]turn, error) {
	rows, err := database.Query(`
		SELECT `+eventColumns+`
		FROM agent_events e
		WHERE e.tool_name = 'UserQuery'
		  AND NOT EXISTS (
		    SELECT 1 FROM otel_signals s
		    WHERE s.kind = 'span' AND s.canonical = 'interaction'
		      AND s.session_id = e.session_id
		      AND (
		        (e.step_id IS NOT NULL AND e.step_id != '' AND s.trace_id = e.step_id)
		        OR ABS(s.ts_micros - CAST(strftime('%s', e.timestamp) AS INTEGER) * 1000000) < ?
		      )
		  )
		ORDER BY e.timestamp DESC
		LIMIT ?`, hookOtelDedupWindowMicros, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []turn
	for rows.Next() {
		evt := scanEvent(rows)
		if evt == nil {
			continue
		}

		sessionID, _ := evt["session_id"].(string)
		eventID, _ := evt["event_id"].(string)

		children := fetchChildren(database, eventID, sessionID, 1)
		stats := computeStats(children)

		turns = append(turns, turn{
			SessionID: sessionID,
			UserQuery: evt,
			Children:  children,
			Stats:     stats,
		})
	}

	if turns == nil {
		return []turn{}, nil
	}
	return turns, nil
}

// extractPromptText pulls the user prompt string from an interaction span's
// attrs_json. Tries attrs.user_prompt then attrs.prompt.
func extractPromptText(attrsRaw string) string {
	if attrsRaw == "" || attrsRaw == "{}" {
		return ""
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsRaw), &attrs); err != nil {
		return ""
	}
	for _, key := range []string{"user_prompt", "prompt", "text"} {
		if v, ok := attrs[key]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// fetchPromptFromTrace looks up the user_prompt log record that shares the
// given trace_id and extracts the prompt text from its attrs_json.
func fetchPromptFromTrace(database *sql.DB, traceID string) string {
	var attrsRaw string
	err := database.QueryRow(`
		SELECT COALESCE(attrs_json, '{}')
		FROM otel_signals
		WHERE trace_id = ? AND canonical = 'user_prompt' AND kind = 'log'
		LIMIT 1`, traceID).Scan(&attrsRaw)
	if err != nil {
		return ""
	}
	return extractPromptText(attrsRaw)
}

func nextPromptBoundary(anchors []otelPromptAnchor, signalID string) int64 {
	for i, anchor := range anchors {
		if anchor.SignalID != signalID {
			continue
		}
		if i+1 < len(anchors) {
			return anchors[i+1].TSMicros
		}
		break
	}
	return 0
}

// fetchChildrenForOtelTurn returns the hook-based children for a turn
// identified by its OTel trace_id. It looks for the matching UserQuery
// hook event in the same session whose step_id matches the trace, or
// falls back to finding the nearest UserQuery by session.
//
// When no matching hook event exists (pure OTel session), returns an
// empty slice — the frontend renders OTel spans via /api/otel/spans.
func fetchChildrenForOtelTurn(database *sql.DB, traceID, sessionID string) []map[string]any {
	// Try to find the UserQuery hook event for this trace.
	var hookEventID string
	_ = database.QueryRow(`
		SELECT event_id FROM agent_events
		WHERE session_id = ? AND tool_name = 'UserQuery'
		  AND (step_id = ? OR step_id IS NULL OR step_id = '')
		LIMIT 1`, sessionID, traceID).Scan(&hookEventID)

	if hookEventID == "" {
		return []map[string]any{}
	}
	return fetchChildren(database, hookEventID, sessionID, 1)
}

// computeOtelStats aggregates per-turn stats from OTel tool spans in the
// same trace. Falls back gracefully to zero values when no spans exist.
func computeOtelStats(database *sql.DB, traceID string) turnStats {
	var toolCount, errorCount int
	_ = database.QueryRow(`
		SELECT
			COUNT(*) AS tool_count,
			SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END) AS error_count
		FROM otel_signals
		WHERE trace_id = ? AND kind = 'span'
		  AND canonical IN ('tool_result', 'tool_execution', 'subagent_invocation')`,
		traceID).Scan(&toolCount, &errorCount)

	return turnStats{
		ToolCount:  toolCount,
		ErrorCount: errorCount,
		Models:     []string{},
	}
}

func fetchOtelLogChildren(database *sql.DB, anchor otelPromptAnchor, windowEnd int64) []map[string]any {
	query := `
		SELECT signal_id, COALESCE(harness, ''), COALESCE(canonical, ''), COALESCE(tool_name, ''),
		       COALESCE(model, ''), ts_micros, COALESCE(duration_ms, 0),
		       success, COALESCE(decision, ''), COALESCE(feature_id, ''),
		       COALESCE(prompt_id, ''), COALESCE(attrs_json, '{}'),
		       COALESCE((SELECT f.title FROM features f WHERE f.id = s.feature_id LIMIT 1), '')
		FROM otel_signals s
		WHERE s.kind = 'log'
		  AND s.session_id = ?
		  AND s.canonical IN ('api_request', 'tool_result', 'tool_decision', 'api_error')`
	args := []any{anchor.SessionID}
	if anchor.PromptID != "" {
		query += ` AND (s.prompt_id = ? OR s.prompt_id = '')`
		args = append(args, anchor.PromptID)
	}
	query += ` ORDER BY s.ts_micros DESC`

	rows, err := database.Query(query, args...)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()

	type otelLogChild struct {
		tsMicros int64
		event    map[string]any
	}
	var children []otelLogChild
	for rows.Next() {
		var signalID, harness, canonical, toolName, model string
		var tsMicros, durationMs int64
		var successVal any
		var decision, featureID, promptID, attrsRaw, featureTitle string

		if err := rows.Scan(
			&signalID, &harness, &canonical, &toolName, &model,
			&tsMicros, &durationMs, &successVal, &decision, &featureID,
			&promptID, &attrsRaw, &featureTitle,
		); err != nil {
			continue
		}

		timestamp, effectiveTSMicros := feedTimestampFromOtel(tsMicros, attrsRaw)
		if effectiveTSMicros < anchor.TSMicros {
			continue
		}
		if windowEnd > 0 && effectiveTSMicros >= windowEnd {
			continue
		}

		summary := otelSummary(canonical, toolName, model, 0, 0, attrsRaw)
		if canonical == "tool_decision" && decision != "" {
			summary = decisionSummary(toolName, decision)
		}
		if canonical == "api_error" && summary == canonical {
			summary = "API error"
		}

		status := "recorded"
		if b, ok := decodeFeedSuccess(successVal); ok && !b {
			status = "failed"
		}
		if canonical == "api_error" {
			status = "failed"
		}

		displayTool := toolName
		if displayTool == "" {
			displayTool = canonical
		}

		children = append(children, otelLogChild{
			tsMicros: effectiveTSMicros,
			event: map[string]any{
				"event_id":        signalID,
				"agent_id":        harness,
				"event_type":      otelLogEventType(canonical, status),
				"timestamp":       timestamp,
				"tool_name":       displayTool,
				"input_summary":   summary,
				"output_summary":  "",
				"session_id":      anchor.SessionID,
				"feature_id":      featureID,
				"feature_title":   featureTitle,
				"status":          status,
				"parent_event_id": anchor.SignalID,
				"subagent_type":   "",
				"tool_use_id":     promptID,
				"model":           model,
				"children":        []map[string]any{},
			},
		})
	}

	if children == nil {
		return []map[string]any{}
	}

	sort.SliceStable(children, func(i, j int) bool {
		return children[i].tsMicros > children[j].tsMicros
	})

	out := make([]map[string]any, 0, len(children))
	for _, child := range children {
		out = append(out, child.event)
	}
	return out
}

func decisionSummary(toolName, decision string) string {
	if toolName == "" {
		return decision
	}
	return toolName + ": " + decision
}

func otelLogEventType(canonical, status string) string {
	if canonical == "api_error" || status == "failed" {
		return "error"
	}
	return "tool_call"
}

// fetchChildren recursively fetches child events up to maxDepth=4 (depth 0-3).
func fetchChildren(database *sql.DB, parentID, sessionID string, depth int) []map[string]any {
	if depth > 3 {
		return nil
	}

	rows, err := database.Query(`
		SELECT `+eventColumns+`
		FROM agent_events e
		WHERE e.parent_event_id = ?
		ORDER BY e.timestamp DESC`, parentID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var children []map[string]any
	for rows.Next() {
		evt := scanEvent(rows)
		if evt == nil {
			continue
		}

		eventID, _ := evt["event_id"].(string)

		// Recurse for direct children.
		evt["children"] = fetchChildren(database, eventID, sessionID, depth+1)

		children = append(children, evt)
	}

	// Suppress duplicate tool_call/Agent rows when a sibling task_delegation exists.
	hasDelegation := false
	for _, c := range children {
		if et, _ := c["event_type"].(string); et == "task_delegation" {
			hasDelegation = true
			break
		}
	}
	if hasDelegation {
		filtered := children[:0]
		for _, c := range children {
			et, _ := c["event_type"].(string)
			tn, _ := c["tool_name"].(string)
			if et == "tool_call" && tn == "Agent" {
				continue // suppress — task_delegation is the canonical row
			}
			filtered = append(filtered, c)
		}
		children = filtered
	}

	return children
}

// scanEvent reads one row from the standard eventColumns projection.
func scanEvent(rows *sql.Rows) map[string]any {
	var eventID, agentID, eventType, ts, toolName string
	var inputSum, outputSum, sessionID, featureID, status string
	var parentEvtID, subagentType, model, stepID, featureTitle string

	if err := rows.Scan(
		&eventID, &agentID, &eventType, &ts, &toolName,
		&inputSum, &outputSum, &sessionID, &featureID, &status,
		&parentEvtID, &subagentType, &model, &stepID, &featureTitle,
	); err != nil {
		return nil
	}

	return map[string]any{
		"event_id":        eventID,
		"agent_id":        agentID,
		"event_type":      eventType,
		"timestamp":       ts,
		"tool_name":       toolName,
		"input_summary":   inputSum,
		"output_summary":  outputSum,
		"session_id":      sessionID,
		"feature_id":      featureID,
		"feature_title":   featureTitle,
		"status":          status,
		"parent_event_id": parentEvtID,
		"subagent_type":   subagentType,
		"tool_use_id":     stepID,
		"model":           model,
	}
}

// computeStats aggregates tool_count, error_count, and distinct models
// from a flat walk of the children tree.
func computeStats(children []map[string]any) turnStats {
	var stats turnStats
	modelSet := make(map[string]bool)
	walkChildren(children, &stats, modelSet)
	for m := range modelSet {
		if m != "" {
			stats.Models = append(stats.Models, m)
		}
	}
	if stats.Models == nil {
		stats.Models = []string{}
	}
	return stats
}

func walkChildren(children []map[string]any, stats *turnStats, models map[string]bool) {
	for _, evt := range children {
		stats.ToolCount++
		evtType, _ := evt["event_type"].(string)
		status, _ := evt["status"].(string)
		if evtType == "error" || status == "failed" {
			stats.ErrorCount++
		}
		if m, ok := evt["model"].(string); ok && m != "" {
			models[m] = true
		}
		if sub, ok := evt["children"].([]map[string]any); ok {
			walkChildren(sub, stats, models)
		}
	}
}
