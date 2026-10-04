package main

import (
	"fmt"
	"sort"
	"time"
)

const (
	toolSchemaVersion = 1
	maxToolLimit      = 100
	defaultToolLimit  = 25
	costSourceShort   = "OTel api_request signals (kind=log only)"
)

// toolMeta is the envelope shared by the sessions, cost and work-item tools.
type toolMeta struct {
	SchemaVersion int           `json:"schema_version"`
	GeneratedAt   string        `json:"generated_at"`
	ElapsedMs     int64         `json:"elapsed_ms"`
	Window        windowInfo    `json:"window"`
	Telemetry     telemetryInfo `json:"telemetry"`
	Notice        string        `json:"notice"`
}

func newToolMeta(now time.Time, hours int, since time.Time, scan telemetryScan, start time.Time) toolMeta {
	m := toolMeta{
		SchemaVersion: toolSchemaVersion,
		GeneratedAt:   now.UTC().Format(time.RFC3339),
		Window:        windowInfo{Since: since.UTC().Format(time.RFC3339), Hours: hours},
		Telemetry:     telemetryInfo{Available: scan.Available, ShardsInWindow: scan.ShardsInWindow, ShardsScanned: scan.ShardsScanned},
		Notice:        untrustedTextNotice,
	}
	if !scan.Available {
		m.Telemetry.Note = telemetryUnavailableNo
	}
	m.ElapsedMs = time.Since(start).Milliseconds()
	return m
}

// ---- wipnote_sessions ------------------------------------------------------

type sessionsInput struct {
	SinceHours int    `json:"since_hours,omitempty" jsonschema:"look-back window in hours (default 24, max 720)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max sessions returned (default 25, max 100)"`
	Sort       string `json:"sort,omitempty" jsonschema:"cost (default) | recent | failures"`
}

type sessionsResult struct {
	toolMeta
	Sort              string             `json:"sort"`
	TotalUSD          float64            `json:"total_usd" jsonschema:"sum over ALL in-window sessions, not just the returned rows"`
	Sessions          []sessionTelemetry `json:"sessions"`
	SessionsTotal     int                `json:"sessions_total"`
	SessionsReturned  int                `json:"sessions_returned"`
	SessionsTruncated bool               `json:"sessions_truncated"`
}

func buildSessions(wipnoteDir string, in sessionsInput, now time.Time) (sessionsResult, error) {
	start := time.Now()
	hours := clampInt(in.SinceHours, defaultSinceHours, maxSinceHours)
	limit := clampInt(in.Limit, defaultToolLimit, maxToolLimit)
	sortBy := in.Sort
	if sortBy == "" {
		sortBy = "cost"
	}
	if sortBy != "cost" && sortBy != "recent" && sortBy != "failures" {
		return sessionsResult{}, fmt.Errorf("sort must be cost, recent or failures (got %q)", in.Sort)
	}
	since := now.Add(-time.Duration(hours) * time.Hour)
	scan, err := scanTelemetry(wipnoteDir, since)
	if err != nil {
		return sessionsResult{}, err
	}
	rows := append([]sessionTelemetry(nil), scan.Sessions...)
	var total float64
	for _, s := range rows {
		total += s.CostUSD
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		switch sortBy {
		case "recent":
			if a.EndedAt != b.EndedAt {
				return a.EndedAt > b.EndedAt
			}
		case "failures":
			if a.FailedToolCalls != b.FailedToolCalls {
				return a.FailedToolCalls > b.FailedToolCalls
			}
			if a.APIErrors != b.APIErrors {
				return a.APIErrors > b.APIErrors
			}
		default:
			if a.CostUSD != b.CostUSD {
				return a.CostUSD > b.CostUSD
			}
		}
		return a.SessionID < b.SessionID
	})
	res := sessionsResult{Sort: sortBy, TotalUSD: roundUSD(total), SessionsTotal: len(rows)}
	if len(rows) > limit {
		rows, res.SessionsTruncated = rows[:limit], true
	}
	res.Sessions = append([]sessionTelemetry{}, rows...)
	res.SessionsReturned = len(res.Sessions)
	res.toolMeta = newToolMeta(now, hours, since, scan, start)
	return res, nil
}

// ---- wipnote_cost ----------------------------------------------------------

type costInput struct {
	GroupBy    string `json:"group_by" jsonschema:"session | model | day | work_item"`
	SinceHours int    `json:"since_hours,omitempty" jsonschema:"look-back window in hours (default 168 = 7 days, max 720)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max buckets returned (default 25, max 100)"`
}

type costBucketOut struct {
	Key              string  `json:"key" jsonschema:"session id, model name, UTC day (YYYY-MM-DD) or work item id; 'unattributed' for cost outside any claim episode"`
	Title            string  `json:"title,omitempty" jsonschema:"work_item grouping only; untrusted, truncated to 80 runes"`
	TotalUSD         float64 `json:"total_usd"`
	APIRequests      int64   `json:"api_requests"`
	TokensIn         int64   `json:"tokens_in"`
	TokensOut        int64   `json:"tokens_out"`
	UnpricedRequests int64   `json:"unpriced_requests"`
}

type costResult struct {
	toolMeta
	GroupBy          string          `json:"group_by"`
	Source           string          `json:"source"`
	TotalUSD         float64         `json:"total_usd" jsonschema:"sum over ALL buckets, not just the returned ones"`
	UnpricedRequests int64           `json:"unpriced_requests"`
	Buckets          []costBucketOut `json:"buckets"`
	BucketsTotal     int             `json:"buckets_total"`
	BucketsReturned  int             `json:"buckets_returned"`
	BucketsTruncated bool            `json:"buckets_truncated"`
	UnattributedUSD  float64         `json:"unattributed_usd,omitempty" jsonschema:"work_item grouping: cost with no overlapping claim episode"`
}

const defaultCostHours = 24 * 7

func buildCost(wipnoteDir string, in costInput, now time.Time) (costResult, error) {
	start := time.Now()
	switch in.GroupBy {
	case "session", "model", "day", "work_item":
	default:
		return costResult{}, fmt.Errorf("group_by must be session, model, day or work_item (got %q)", in.GroupBy)
	}
	hours := clampInt(in.SinceHours, defaultCostHours, maxSinceHours)
	limit := clampInt(in.Limit, defaultToolLimit, maxToolLimit)
	since := now.Add(-time.Duration(hours) * time.Hour)

	var opts scanOptions
	var attr *claimAttribution
	var titles map[string]string
	if in.GroupBy == "work_item" {
		var err error
		if attr, titles, err = loadClaimAttribution(wipnoteDir, true); err != nil {
			return costResult{}, err
		}
		opts.pointsFor = attr.hasSession
	}
	scan, err := scanTelemetryOpts(wipnoteDir, since, opts)
	if err != nil {
		return costResult{}, err
	}

	buckets := map[string]*costBucket{}
	get := func(k string) *costBucket {
		if buckets[k] == nil {
			buckets[k] = &costBucket{}
		}
		return buckets[k]
	}
	var all costBucket
	for _, s := range scan.Sessions {
		switch in.GroupBy {
		case "session":
			b := costBucket{CostUSD: s.CostUSD, Requests: s.APIRequests, TokensIn: s.TokensIn, TokensOut: s.TokensOut, Unpriced: s.UnpricedRequests}
			if b.Requests > 0 {
				get(s.SessionID).add(b)
			}
		case "model":
			for k, b := range s.byModel {
				get(k).add(*b)
			}
		case "day":
			for k, b := range s.byDay {
				get(k).add(*b)
			}
		case "work_item":
			attr.attribute(s, get)
		}
		all.add(costBucket{CostUSD: s.CostUSD, Unpriced: s.UnpricedRequests})
	}

	rows := make([]costBucketOut, 0, len(buckets))
	for k, b := range buckets {
		row := costBucketOut{Key: k, TotalUSD: roundUSD(b.CostUSD), APIRequests: b.Requests,
			TokensIn: b.TokensIn, TokensOut: b.TokensOut, UnpricedRequests: b.Unpriced}
		if in.GroupBy == "work_item" {
			row.Title = cleanTitle(titles[k])
		}
		rows = append(rows, row)
	}
	if in.GroupBy == "day" {
		// Chronological, keeping the most recent days when truncated.
		sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })
	} else {
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].TotalUSD != rows[j].TotalUSD {
				return rows[i].TotalUSD > rows[j].TotalUSD
			}
			return rows[i].Key < rows[j].Key
		})
	}
	res := costResult{GroupBy: in.GroupBy, Source: costSourceShort, TotalUSD: roundUSD(all.CostUSD),
		UnpricedRequests: all.Unpriced, BucketsTotal: len(rows)}
	if len(rows) > limit {
		res.BucketsTruncated = true
		if in.GroupBy == "day" {
			rows = rows[len(rows)-limit:]
		} else {
			rows = rows[:limit]
		}
	}
	if b := buckets[unattributedKey]; b != nil {
		res.UnattributedUSD = roundUSD(b.CostUSD)
	}
	res.Buckets = rows
	res.BucketsReturned = len(rows)
	res.toolMeta = newToolMeta(now, hours, since, scan, start)
	return res, nil
}
