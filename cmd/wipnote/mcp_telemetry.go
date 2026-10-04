package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tidwall/gjson"
)

// shardFileName is the per-session telemetry shard written by the NDJSON sink
// (observe/otel/sink/ndjson): <sessionsDir>/<wipnote-session-id>/events.ndjson.
const shardFileName = "events.ndjson"

// maxTelemetryShards bounds how many session shards one tool call scans, newest
// first. Scan cost is linear in shard bytes, so the cap is what keeps a call
// inside its budget on a corpus with thousands of sessions.
const maxTelemetryShards = 300

// maxShardLineBytes bounds one NDJSON line. Longer lines are skipped, not
// fatal: a single oversized attribute bag must not hide a whole session.
const maxShardLineBytes = 16 << 20

// sessionTelemetry is one session's OTel aggregates. Every field is a count,
// duration or USD figure: no prompt text, tool input/output or error messages
// are ever read into a field, so none can reach a tool result.
type sessionTelemetry struct {
	SessionID        string  `json:"session_id" jsonschema:"wipnote session id (the telemetry shard name)"`
	StartedAt        string  `json:"started_at,omitempty" jsonschema:"RFC3339 time of the first signal in the window"`
	EndedAt          string  `json:"ended_at,omitempty" jsonschema:"RFC3339 time of the last signal in the window"`
	CostUSD          float64 `json:"cost_usd" jsonschema:"sum of api_request cost_usd (OTel, vendor-reported or derived)"`
	UnpricedRequests int64   `json:"unpriced_requests" jsonschema:"api_request signals with no cost_usd"`
	APIRequests      int64   `json:"api_requests"`
	TokensIn         int64   `json:"tokens_in"`
	TokensOut        int64   `json:"tokens_out"`
	ToolCalls        int64   `json:"tool_calls"`
	FailedToolCalls  int64   `json:"failed_tool_calls"`
	PermissionWaits  int64   `json:"permission_waits" jsonschema:"tool calls that blocked on a user decision"`
	PermissionWaitMs int64   `json:"permission_wait_ms"`
	APIErrors        int64   `json:"api_errors"`
	Compactions      int64   `json:"compactions"`
	Subagents        int64   `json:"subagent_invocations"`
}

// telemetryScan is the result of scanning the in-window shards.
type telemetryScan struct {
	Sessions       []sessionTelemetry
	ShardsInWindow int
	ShardsScanned  int
	Available      bool
}

type shardFile struct {
	id    string
	mtime time.Time
}

// recentShards lists session shards whose events.ndjson was modified inside the
// window, newest first. One ReadDir plus one Stat per session directory — a
// filesystem walk, not a database loop.
func recentShards(sessionsDir string, since time.Time) ([]shardFile, error) {
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []shardFile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		fi, err := os.Stat(filepath.Join(sessionsDir, e.Name(), shardFileName))
		if err != nil || fi.ModTime().Before(since) {
			continue
		}
		out = append(out, shardFile{id: e.Name(), mtime: fi.ModTime()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mtime.After(out[j].mtime) })
	return out, nil
}

// scanTelemetry aggregates per-session figures straight from the canonical
// NDJSON shards. Nothing is ingested or materialised: this deliberately avoids
// the dashboard's replay-everything-into-memory indexer, which is what makes
// `wipnote serve` hang on large histories. Shards are scanned in parallel, one
// goroutine per core, each in a single streaming pass.
func scanTelemetry(wipnoteDir string, since time.Time) (telemetryScan, error) {
	sessionsDir := filepath.Join(wipnoteDir, "sessions")
	shards, err := recentShards(sessionsDir, since)
	if err != nil {
		return telemetryScan{}, fmt.Errorf("list session shards: %w", err)
	}
	scan := telemetryScan{ShardsInWindow: len(shards), Sessions: []sessionTelemetry{}}
	if len(shards) == 0 {
		return scan, nil
	}
	if len(shards) > maxTelemetryShards {
		shards = shards[:maxTelemetryShards]
	}
	scan.Available = true
	scan.ShardsScanned = len(shards)

	type result struct {
		st sessionTelemetry
		ok bool
	}
	results := make([]result, len(shards))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < min(runtime.NumCPU(), 8); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				path := filepath.Join(sessionsDir, shards[i].id, shardFileName)
				st, ok := aggregateShard(path, shards[i].id, since)
				results[i] = result{st, ok}
			}
		}()
	}
	for i := range shards {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, r := range results {
		if r.ok {
			scan.Sessions = append(scan.Sessions, r.st)
		}
	}
	return scan, nil
}

// shardPaths are the only JSON fields read from a line. attrs / resource_attrs
// (where prompt text and tool I/O live) are never extracted; gjson skips over
// them without decoding.
var shardPaths = []string{
	"canonical", "kind", "ts", "cost_usd", "tokens_input", "tokens_output",
	"tool_use_id", "signal_id", "success", "duration_ms",
}

// aggregateShard streams one shard once. ok is false when the shard is
// unreadable or has no signals inside the window. Tool calls are de-duplicated
// on tool_use_id because Claude emits both a span and a log per tool call.
func aggregateShard(path, id string, since time.Time) (sessionTelemetry, bool) {
	st := sessionTelemetry{SessionID: id}
	f, err := os.Open(path)
	if err != nil {
		return st, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), maxShardLineBytes)

	tools, failed, blocked := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	var first, last time.Time
	for sc.Scan() {
		v := gjson.GetManyBytes(sc.Bytes(), shardPaths...)
		ts, err := time.Parse(time.RFC3339Nano, v[2].String())
		if err != nil || ts.Before(since) {
			continue
		}
		if first.IsZero() || ts.Before(first) {
			first = ts
		}
		if ts.After(last) {
			last = ts
		}
		key := strings.Clone(v[6].String())
		if key == "" {
			key = strings.Clone(v[7].String())
		}
		applySignal(&st, v, key, tools, failed, blocked)
	}
	if first.IsZero() {
		return st, false
	}
	st.ToolCalls, st.FailedToolCalls, st.PermissionWaits = int64(len(tools)), int64(len(failed)), int64(len(blocked))
	st.CostUSD = roundUSD(st.CostUSD)
	st.StartedAt = first.UTC().Format(time.RFC3339)
	st.EndedAt = last.UTC().Format(time.RFC3339)
	return st, true
}

// applySignal folds one decoded line into the session aggregates. v follows
// shardPaths order; key is the de-duplication key (tool_use_id, else signal_id).
func applySignal(st *sessionTelemetry, v []gjson.Result, key string, tools, failed, blocked map[string]struct{}) {
	switch v[0].String() {
	case "api_request":
		if v[1].String() != "log" { // the span twin carries the same cost; count once
			return
		}
		st.APIRequests++
		st.TokensIn += v[4].Int()
		st.TokensOut += v[5].Int()
		if v[3].Exists() {
			st.CostUSD += v[3].Float()
		} else {
			st.UnpricedRequests++
		}
	case "tool_result":
		tools[key] = struct{}{}
		if v[8].Exists() && !v[8].Bool() {
			failed[key] = struct{}{}
		}
	case "subagent_invocation":
		if _, seen := tools[key]; !seen {
			st.Subagents++
		}
		tools[key] = struct{}{}
	case "tool_blocked_on_user":
		if _, seen := blocked[key]; !seen {
			st.PermissionWaitMs += v[9].Int()
		}
		blocked[key] = struct{}{}
	case "api_error":
		st.APIErrors++
	case "compaction":
		st.Compactions++
	}
}

// roundUSD trims float accumulation noise (24.984999999999992) to micro-dollars.
func roundUSD(v float64) float64 { return math.Round(v*1e6) / 1e6 }
