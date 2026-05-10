package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// feedEvent is the unified wire shape for /api/events/feed.
// source is "otel" for OTel-derived events, "hook" for agent_events fallback.
type feedEvent struct {
	ID           string  `json:"id"`
	Source       string  `json:"source"`
	Type         string  `json:"type"`
	Harness      string  `json:"harness,omitempty"`
	ToolName     string  `json:"tool_name,omitempty"`
	Model        string  `json:"model,omitempty"`
	Timestamp    string  `json:"timestamp"`
	DurationMs   int64   `json:"duration_ms,omitempty"`
	TokensIn     int64   `json:"tokens_in,omitempty"`
	TokensOut    int64   `json:"tokens_out,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
	Success      *bool   `json:"success,omitempty"`
	Decision     string  `json:"decision,omitempty"`
	SessionID    string  `json:"session_id,omitempty"`
	FeatureID    string  `json:"feature_id,omitempty"`
	TraceID      string  `json:"trace_id,omitempty"`
	ParentSpan   string  `json:"parent_span,omitempty"`
	Summary      string  `json:"summary,omitempty"`
	FeatureTitle string  `json:"feature_title,omitempty"`
	// tsMicros is used internally for sorting and is not serialised.
	tsMicros int64
}

// eventsFeedHandler returns a unified activity feed combining OTel signals
// (primary) and hook-only agent_events (fallback for event types with no OTel
// equivalent). Results are merged and sorted by timestamp DESC.
//
// GET /api/events/feed?limit=N  (default 50, max 200)
func eventsFeedHandler(database *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		limit := 50
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}

		otelEvents, err := queryOtelFeedEvents(database, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		hookEvents, err := queryHookFeedEvents(database, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		messageEvents, err := queryMessageFeedEvents(database, limit)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Deduplicate messageEvents against otelEvents to avoid showing
		// the same assistant response twice (once from otel_signals, once from messages).
		deduped := deduplicateMessageEvents(otelEvents, messageEvents)
		merged := merge(append(otelEvents, deduped...), hookEvents, limit)
		respondJSON(w, map[string]any{"events": merged})
	}
}

// queryOtelFeedEvents fetches relevant OTel spans and returns them as feedEvents.
func queryOtelFeedEvents(database *sql.DB, limit int) ([]feedEvent, error) {
	rows, err := database.Query(`
		SELECT s.signal_id, COALESCE(s.harness, ''), COALESCE(s.trace_id, ''), COALESCE(s.parent_span, ''),
		       s.canonical, COALESCE(s.tool_name, '') AS tool_name,
		       COALESCE(s.model, '') AS model,
		       s.ts_micros, COALESCE(s.duration_ms, 0) AS duration_ms,
		       COALESCE(s.tokens_in, 0), COALESCE(s.tokens_out, 0),
		       COALESCE(s.cost_usd, 0) AS cost_usd,
		       s.success, COALESCE(s.decision, '') AS decision,
		       COALESCE(s.session_id, '') AS session_id,
		       COALESCE(s.feature_id, '') AS feature_id,
		       COALESCE((SELECT f.title FROM features f WHERE f.id = s.feature_id LIMIT 1), '') AS feature_title,
		       COALESCE(s.attrs_json, '{}') AS attrs_json
		FROM otel_signals s
		WHERE (
			(s.kind = 'span' AND s.canonical IN (
		      'interaction', 'api_request', 'tool_result',
		      'tool_execution', 'tool_blocked_on_user', 'subagent_invocation'
		    ))
			OR
			(s.kind = 'log' AND s.canonical IN (
		      'user_prompt', 'assistant_text', 'api_request', 'tool_result', 'tool_decision'
		    ))
		)
		ORDER BY s.ts_micros DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []feedEvent
	for rows.Next() {
		var ev feedEvent
		var successVal any
		var attrsRaw string
		var tsMicros int64

		if err := rows.Scan(
			&ev.ID, &ev.Harness, &ev.TraceID, &ev.ParentSpan,
			&ev.Type, &ev.ToolName, &ev.Model,
			&tsMicros, &ev.DurationMs,
			&ev.TokensIn, &ev.TokensOut, &ev.CostUSD,
			&successVal, &ev.Decision,
			&ev.SessionID, &ev.FeatureID, &ev.FeatureTitle,
			&attrsRaw,
		); err != nil {
			continue
		}

		if b, ok := decodeFeedSuccess(successVal); ok {
			ev.Success = &b
		}

		ev.Source = "otel"
		ev.Timestamp, ev.tsMicros = feedTimestampFromOtel(tsMicros, attrsRaw)
		ev.Summary = otelSummary(ev.Type, ev.ToolName, ev.Model, ev.TokensIn, ev.TokensOut, attrsRaw)

		// Zero out empty optional fields to keep JSON tidy.
		if ev.Model == "" {
			ev.Model = ""
		}
		out = append(out, ev)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].tsMicros == out[j].tsMicros {
			return out[i].ID > out[j].ID
		}
		return out[i].tsMicros > out[j].tsMicros
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func queryMessageFeedEvents(database *sql.DB, limit int) ([]feedEvent, error) {
	rows, err := database.Query(`
		SELECT m.id,
			COALESCE(NULLIF(m.agent_id, ''), s.agent_assigned, ''),
			COALESCE(m.timestamp, ''),
			COALESCE(m.content, ''),
			COALESCE(m.model, ''),
			m.session_id
		FROM messages m
		LEFT JOIN sessions s ON s.session_id = m.session_id
		WHERE m.role = 'assistant' AND TRIM(m.content) != ''
		ORDER BY m.timestamp DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []feedEvent{}
	for rows.Next() {
		var id int64
		var harness, tsRaw, content, model, sessionID string
		if err := rows.Scan(&id, &harness, &tsRaw, &content, &model, &sessionID); err != nil {
			return nil, err
		}
		tsMicros := timestampStringToMicros(tsRaw)
		if tsMicros == 0 {
			continue
		}
		out = append(out, feedEvent{
			ID:        "message:" + strconv.FormatInt(id, 10),
			Source:    "message",
			Type:      "assistant_text",
			Harness:   harness,
			Model:     model,
			Timestamp: time.UnixMicro(tsMicros).UTC().Format(time.RFC3339),
			SessionID: sessionID,
			Summary:   truncateFeedText(content, 200),
			tsMicros:  tsMicros,
		})
	}
	return out, rows.Err()
}

func feedTimestampFromOtel(tsMicros int64, attrsRaw string) (string, int64) {
	if tsMicros > 0 {
		return time.UnixMicro(tsMicros).UTC().Format(time.RFC3339), tsMicros
	}

	var attrs map[string]any
	if err := json.Unmarshal([]byte(attrsRaw), &attrs); err == nil {
		if raw, ok := attrs["event.timestamp"]; ok {
			if parsed, ok := parseFeedEventTimestamp(raw); ok {
				return parsed.UTC().Format(time.RFC3339), parsed.UnixMicro()
			}
		}
	}

	return time.UnixMicro(tsMicros).UTC().Format(time.RFC3339), tsMicros
}

func parseFeedEventTimestamp(v any) (time.Time, bool) {
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

func decodeFeedSuccess(v any) (bool, bool) {
	switch x := v.(type) {
	case nil:
		return false, false
	case int64:
		return x == 1, true
	case int:
		return x == 1, true
	case bool:
		return x, true
	case []byte:
		switch strings.ToLower(string(x)) {
		case "1", "true":
			return true, true
		case "0", "false":
			return false, true
		}
	case string:
		switch strings.ToLower(x) {
		case "1", "true":
			return true, true
		case "0", "false":
			return false, true
		}
	}
	return false, false
}

// queryHookFeedEvents fetches hook-only event types from agent_events.
func queryHookFeedEvents(database *sql.DB, limit int) ([]feedEvent, error) {
	rows, err := database.Query(`
		SELECT e.event_id, e.event_type, e.timestamp,
		       COALESCE(e.tool_name, '') AS tool_name,
		       COALESCE(e.input_summary, '') AS input_summary,
		       COALESCE(e.output_summary, '') AS output_summary,
		       COALESCE(e.session_id, '') AS session_id,
		       COALESCE(e.feature_id, '') AS feature_id,
		       COALESCE(e.parent_event_id, '') AS parent_event_id,
		       COALESCE((SELECT f.title FROM features f WHERE f.id = e.feature_id LIMIT 1), '') AS feature_title
		FROM agent_events e
		WHERE e.event_type IN ('start', 'end', 'check_point', 'error', 'tool_call')
		ORDER BY e.timestamp DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []feedEvent
	for rows.Next() {
		var ev feedEvent
		var ts, inputSum, outputSum, parentEvtID string

		if err := rows.Scan(
			&ev.ID, &ev.Type, &ts,
			&ev.ToolName, &inputSum, &outputSum,
			&ev.SessionID, &ev.FeatureID, &parentEvtID, &ev.FeatureTitle,
		); err != nil {
			continue
		}

		ev.Source = "hook"
		ev.Timestamp = ts
		ev.Summary = hookSummary(outputSum, inputSum)

		// Parse timestamp for sorting; tolerate missing values.
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			ev.tsMicros = t.UnixMicro()
		} else if t, err := time.Parse("2006-01-02T15:04:05.999999999Z07:00", ts); err == nil {
			ev.tsMicros = t.UnixMicro()
		}

		out = append(out, ev)
	}
	return out, nil
}

// otelSummary produces a human-readable one-liner for an OTel span.
func otelSummary(canonical, toolName, model string, tokensIn, tokensOut int64, attrsRaw string) string {
	// Parse attrs once — only if we need content-specific details.
	var attrs map[string]any
	if attrsRaw != "" && attrsRaw != "{}" {
		_ = json.Unmarshal([]byte(attrsRaw), &attrs)
	}

	pull := func(k string) string {
		if attrs == nil {
			return ""
		}
		if v, ok := attrs[k]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
		return ""
	}

	truncate := func(s string, n int) string {
		if len(s) <= n {
			return s
		}
		return s[:n] + "..."
	}

	switch canonical {
	case "tool_result", "tool_execution", "subagent_invocation":
		switch strings.ToLower(toolName) {
		case "bash":
			if cmd := pull("full_command"); cmd != "" {
				return truncate(cmd, 200)
			}
			if cmd := pull("bash_command"); cmd != "" {
				return truncate(cmd, 200)
			}
			if input := pull("input"); input != "" {
				return truncate(input, 200)
			}
			return "Bash"
		case "read", "edit", "write", "notebookedit":
			if fp := pull("file_path"); fp != "" {
				return fp
			}
			return toolName
		case "task", "agent":
			if desc := pull("description"); desc != "" {
				return truncate(desc, 200)
			}
			if prompt := pull("prompt"); prompt != "" {
				return truncate(prompt, 200)
			}
			return toolName
		default:
			if toolName != "" {
				return toolName
			}
			return canonical
		}
	case "api_request":
		if model != "" {
			if tokensIn > 0 || tokensOut > 0 {
				return model + " — " + formatTokens(tokensIn, tokensOut)
			}
			return model
		}
		return "API request"
	case "interaction":
		if turn := pull("turn"); turn != "" {
			return "User prompt (turn " + turn + ")"
		}
		return "User prompt"
	case "assistant_text":
		if text := pull("text"); text != "" {
			return truncateFeedText(text, 200)
		}
		return "Assistant response"
	case "tool_blocked_on_user":
		return "Permission request"
	default:
		return canonical
	}
}

// hookSummary picks the best human-readable text from hook event fields.
func hookSummary(outputSummary, inputSummary string) string {
	if outputSummary != "" {
		return outputSummary
	}
	return inputSummary
}

func truncateFeedText(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// formatTokens returns a concise token count string like "1.2k in / 512 out".
func formatTokens(in, out int64) string {
	return formatNum(in) + " in / " + formatNum(out) + " out"
}

func formatNum(n int64) string {
	if n >= 1000 {
		f := float64(n) / 1000.0
		// One decimal place only when meaningful.
		s := strconv.FormatFloat(f, 'f', 1, 64)
		// Strip trailing ".0"
		s = strings.TrimSuffix(s, ".0")
		return s + "k"
	}
	return strconv.FormatInt(n, 10)
}

// merge combines two slices of feedEvent, sorts by tsMicros DESC, and
// returns at most limit items.
func merge(a, b []feedEvent, limit int) []feedEvent {
	combined := make([]feedEvent, 0, len(a)+len(b))
	combined = append(combined, a...)
	combined = append(combined, b...)
	sort.Slice(combined, func(i, j int) bool {
		return combined[i].tsMicros > combined[j].tsMicros
	})
	if len(combined) > limit {
		combined = combined[:limit]
	}
	return combined
}

func legacyEventFromFeed(ev feedEvent) map[string]any {
	toolName := ev.ToolName
	if toolName == "" && ev.Type == "interaction" {
		toolName = "UserQuery"
	}

	status := "recorded"
	if ev.Type == "tool_blocked_on_user" {
		status = "blocked"
	} else if ev.Success != nil {
		if *ev.Success {
			status = "completed"
		} else {
			status = "failed"
		}
	}

	return map[string]any{
		"event_id":        ev.ID,
		"agent_id":        legacyAgentID(ev),
		"event_type":      ev.Type,
		"timestamp":       ev.Timestamp,
		"tool_name":       toolName,
		"input_summary":   ev.Summary,
		"output_summary":  ev.Summary,
		"session_id":      ev.SessionID,
		"feature_id":      ev.FeatureID,
		"feature_title":   ev.FeatureTitle,
		"parent_event_id": ev.ParentSpan,
		"status":          status,
	}
}

func legacyAgentID(ev feedEvent) string {
	switch ev.Harness {
	case "claude_code":
		return "claude-code"
	case "gemini_cli":
		return "gemini"
	case "codex", "wipnote":
		return ev.Harness
	}
	switch ev.Source {
	case "otel":
		return "otel"
	case "hook":
		return "hook"
	default:
		return ""
	}
}

// deduplicateMessageEvents filters out message table entries that have a
// corresponding otel_signals assistant_text event. A match is detected when
// the session_id and type are both "assistant_text" and timestamps are within
// ±5 seconds of each other.
func deduplicateMessageEvents(otelEvents, messageEvents []feedEvent) []feedEvent {
	// Build a set of (session_id, timestamp_micros) pairs from otel events.
	// We use a 5-second window to account for minor timestamp skew.
	const windowMicros int64 = 5_000_000 // 5 seconds in microseconds

	otelSet := make(map[string]struct{}) // key: "session_id:type"
	otelTimestamps := make(map[string][]int64)

	for _, ev := range otelEvents {
		if ev.Type == "assistant_text" && ev.SessionID != "" {
			key := ev.SessionID + ":" + ev.Type
			otelSet[key] = struct{}{}
			otelTimestamps[key] = append(otelTimestamps[key], ev.tsMicros)
		}
	}

	var deduped []feedEvent
	for _, msg := range messageEvents {
		if msg.Type != "assistant_text" || msg.SessionID == "" {
			// Keep non-assistant or missing session messages.
			deduped = append(deduped, msg)
			continue
		}

		key := msg.SessionID + ":" + msg.Type
		if _, found := otelSet[key]; !found {
			// No otel event for this session+type; keep the message event.
			deduped = append(deduped, msg)
			continue
		}

		// Check if any otel timestamp is within ±5 seconds of this message.
		isDuplicate := false
		for _, otelTs := range otelTimestamps[key] {
			if diff := msg.tsMicros - otelTs; diff >= -windowMicros && diff <= windowMicros {
				isDuplicate = true
				break
			}
		}

		if !isDuplicate {
			// No otel event within ±5s; keep the message event.
			deduped = append(deduped, msg)
		}
	}

	return deduped
}
