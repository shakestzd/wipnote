package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const (
	defaultTraceTopN  = 10
	maxTraceTopN      = 20
	maxCompactionList = 50
)

type traceInput struct {
	SessionID string `json:"session_id" jsonschema:"wipnote session id (telemetry shard name), as returned by wipnote_sessions"`
	TopN      int    `json:"top_n,omitempty" jsonschema:"max slowest tools / agent types returned (default 10, max 20)"`
}

type toolStat struct {
	Name     string `json:"name" jsonschema:"tool name; restricted to [A-Za-z0-9_.:/-], max 64 chars"`
	Calls    int64  `json:"calls"`
	Failures int64  `json:"failures"`
	P50Ms    int64  `json:"p50_ms"`
	MaxMs    int64  `json:"max_ms"`
	TotalMs  int64  `json:"total_ms"`
}

type agentStat struct {
	AgentType   string `json:"agent_type" jsonschema:"restricted to [A-Za-z0-9_.:/-], max 64 chars; 'unknown' when the signal carries none"`
	Invocations int64  `json:"invocations"`
	Failures    int64  `json:"failures"`
	TotalMs     int64  `json:"total_ms"`
}

type modelCost struct {
	Model            string  `json:"model"`
	TotalUSD         float64 `json:"total_usd"`
	APIRequests      int64   `json:"api_requests"`
	TokensIn         int64   `json:"tokens_in"`
	TokensOut        int64   `json:"tokens_out"`
	UnpricedRequests int64   `json:"unpriced_requests"`
}

type traceResult struct {
	toolMeta
	Session              sessionTelemetry `json:"session" jsonschema:"whole-shard aggregates (no time window)"`
	SlowestTools         []toolStat       `json:"slowest_tools" jsonschema:"top_n tools by max_ms"`
	ToolsTotal           int              `json:"tools_total" jsonschema:"distinct tool names in the session"`
	ToolsTruncated       bool             `json:"tools_truncated"`
	CompactionTimes      []string         `json:"compaction_times" jsonschema:"RFC3339, oldest first, capped at 50"`
	CompactionsTruncated bool             `json:"compactions_truncated"`
	Subagents            []agentStat      `json:"subagents" jsonschema:"subagent invocations grouped by agent type"`
	SubagentsTotal       int              `json:"subagent_types_total"`
	SubagentsTruncated   bool             `json:"subagents_truncated"`
	CostByModel          []modelCost      `json:"cost_by_model"`
	ModelsTotal          int              `json:"models_total"`
	ModelsTruncated      bool             `json:"models_truncated"`
}

// tracePaths are the only fields read from a line. attrs is never read.
var tracePaths = []string{
	"canonical", "kind", "ts", "cost_usd", "tokens_input", "tokens_output", "tool_name",
	"tool_use_id", "signal_id", "success", "duration_ms", "model", "native", "agent_type",
}

// validSessionID rejects anything that is not a plain shard directory name, so
// the id cannot walk out of .wipnote/sessions.
func validSessionID(id string) bool {
	return id != "" && id != "." && id != ".." && id == filepath.Base(id) &&
		!strings.ContainsAny(id, `/\`) && !strings.ContainsRune(id, 0)
}

func buildSessionTrace(wipnoteDir string, in traceInput, now time.Time) (traceResult, error) {
	start := time.Now()
	if !validSessionID(in.SessionID) {
		return traceResult{}, fmt.Errorf("invalid session_id")
	}
	topN := clampInt(in.TopN, defaultTraceTopN, maxTraceTopN)
	path := filepath.Join(wipnoteDir, "sessions", in.SessionID, shardFileName)
	if _, err := os.Stat(path); err != nil {
		return traceResult{}, fmt.Errorf("no telemetry shard for session %q", in.SessionID)
	}
	zero := time.Time{}
	st, ok := aggregateShard(path, in.SessionID, zero, false)
	res := traceResult{
		Session: st, SlowestTools: []toolStat{}, CompactionTimes: []string{},
		Subagents: []agentStat{}, CostByModel: []modelCost{},
	}
	if !ok {
		st = sessionTelemetry{SessionID: in.SessionID}
		res.Session = st
	}
	f, err := os.Open(path)
	if err != nil {
		return traceResult{}, fmt.Errorf("open shard: %w", err)
	}
	defer f.Close()
	acc := newTraceAcc()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), maxShardLineBytes)
	for sc.Scan() {
		acc.add(gjson.GetManyBytes(sc.Bytes(), tracePaths...), sc.Bytes())
	}
	acc.fill(&res, topN)
	for k, b := range st.byModel {
		res.CostByModel = append(res.CostByModel, modelCost{Model: k, TotalUSD: roundUSD(b.CostUSD),
			APIRequests: b.Requests, TokensIn: b.TokensIn, TokensOut: b.TokensOut, UnpricedRequests: b.Unpriced})
	}
	sort.Slice(res.CostByModel, func(i, j int) bool {
		a, b := res.CostByModel[i], res.CostByModel[j]
		if a.TotalUSD != b.TotalUSD {
			return a.TotalUSD > b.TotalUSD
		}
		return a.Model < b.Model
	})
	res.ModelsTotal = len(res.CostByModel)
	if len(res.CostByModel) > topN {
		res.CostByModel, res.ModelsTruncated = res.CostByModel[:topN], true
	}
	res.Session.byModel, res.Session.byDay = nil, nil
	hours := 0
	res.toolMeta = newToolMeta(now, hours, zero, telemetryScan{Available: ok, ShardsInWindow: 1, ShardsScanned: 1}, start)
	res.Window = windowInfo{}
	return res, nil
}

// traceAcc folds one shard's tool, compaction and subagent signals. Tool calls
// and subagent invocations are de-duplicated on tool_use_id (Claude emits a
// span and a log per call); the longest duration and any failure win.
type traceAcc struct {
	tools       map[string]*traceCall
	agents      map[string]*traceCall
	compactions []time.Time
}

type traceCall struct {
	name   string
	ms     int64
	failed bool
}

func newTraceAcc() *traceAcc {
	return &traceAcc{tools: map[string]*traceCall{}, agents: map[string]*traceCall{}}
}

func (a *traceAcc) add(v []gjson.Result, line []byte) {
	canon := v[0].String()
	key := strings.Clone(v[7].String())
	if key == "" {
		key = strings.Clone(v[8].String())
	}
	switch canon {
	case "tool_result":
		mergeCall(a.tools, key, cleanName(v[6].String()), v[10].Int(), v[9].Exists() && !v[9].Bool())
	case "subagent_invocation":
		name := cleanName(v[13].String())
		if name == "" { // not a top-level field today; fall back to the attribute bag (type name only)
			name = cleanName(gjson.GetBytes(line, "attrs.agent_type").String())
		}
		if name == "" {
			name = cleanName(gjson.GetBytes(line, "attrs.subagent_type").String())
		}
		if name == "" {
			name = cleanName(v[6].String())
		}
		if name == "" {
			name = "unknown"
		}
		mergeCall(a.agents, key, name, v[10].Int(), v[9].Exists() && !v[9].Bool())
	case "compaction":
		if ts, err := time.Parse(time.RFC3339Nano, v[2].String()); err == nil {
			a.compactions = append(a.compactions, ts)
		}
	}
}

func mergeCall(m map[string]*traceCall, key, name string, ms int64, failed bool) {
	c := m[key]
	if c == nil {
		if name == "" {
			name = "unknown"
		}
		c = &traceCall{name: name}
		m[key] = c
	}
	if c.name == "unknown" && name != "" {
		c.name = name
	}
	if ms > c.ms {
		c.ms = ms
	}
	c.failed = c.failed || failed
}

func (a *traceAcc) fill(res *traceResult, topN int) {
	perTool := map[string][]int64{}
	fails := map[string]int64{}
	for _, c := range a.tools {
		perTool[c.name] = append(perTool[c.name], c.ms)
		if c.failed {
			fails[c.name]++
		}
	}
	for name, ds := range perTool {
		sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
		var total int64
		for _, d := range ds {
			total += d
		}
		res.SlowestTools = append(res.SlowestTools, toolStat{Name: name, Calls: int64(len(ds)), Failures: fails[name],
			P50Ms: ds[(len(ds)-1)/2], MaxMs: ds[len(ds)-1], TotalMs: total})
	}
	sort.Slice(res.SlowestTools, func(i, j int) bool {
		x, y := res.SlowestTools[i], res.SlowestTools[j]
		if x.MaxMs != y.MaxMs {
			return x.MaxMs > y.MaxMs
		}
		return x.Name < y.Name
	})
	res.ToolsTotal = len(res.SlowestTools)
	if len(res.SlowestTools) > topN {
		res.SlowestTools, res.ToolsTruncated = res.SlowestTools[:topN], true
	}

	byAgent := map[string]*agentStat{}
	for _, c := range a.agents {
		s := byAgent[c.name]
		if s == nil {
			s = &agentStat{AgentType: c.name}
			byAgent[c.name] = s
		}
		s.Invocations++
		s.TotalMs += c.ms
		if c.failed {
			s.Failures++
		}
	}
	for _, s := range byAgent {
		res.Subagents = append(res.Subagents, *s)
	}
	sort.Slice(res.Subagents, func(i, j int) bool {
		x, y := res.Subagents[i], res.Subagents[j]
		if x.Invocations != y.Invocations {
			return x.Invocations > y.Invocations
		}
		return x.AgentType < y.AgentType
	})
	res.SubagentsTotal = len(res.Subagents)
	if len(res.Subagents) > topN {
		res.Subagents, res.SubagentsTruncated = res.Subagents[:topN], true
	}

	sort.Slice(a.compactions, func(i, j int) bool { return a.compactions[i].Before(a.compactions[j]) })
	if len(a.compactions) > maxCompactionList {
		a.compactions, res.CompactionsTruncated = a.compactions[len(a.compactions)-maxCompactionList:], true
	}
	for _, t := range a.compactions {
		res.CompactionTimes = append(res.CompactionTimes, t.UTC().Format(time.RFC3339))
	}
}
