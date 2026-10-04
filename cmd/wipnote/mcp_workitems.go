package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/graph"
)

const unattributedKey = "unattributed"

// claimAttribution joins telemetry to work items through claim-episode
// intervals, entirely in memory: episodes are indexed by session once, then
// every cost point is a lookup. No per-work-item query exists.
type claimAttribution struct {
	bySession map[string][]claimledger.Episode
	// episodes and openHolders are per work item, for the work-items tool.
	episodes    map[string]int
	openHolders map[string]int
}

// loadClaimAttribution reads every claim ledger (one HTML file per root
// session plus the archive) and the work-item titles.
func loadClaimAttribution(wipnoteDir string, wantTitles bool) (*claimAttribution, map[string]string, error) {
	a := &claimAttribution{
		bySession: map[string][]claimledger.Episode{},
		episodes:  map[string]int{}, openHolders: map[string]int{},
	}
	eps, err := claimledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("read claim ledger: %w", err)
	}
	open := map[string]map[string]bool{}
	for _, ep := range eps {
		a.episodes[ep.WorkItemID]++
		a.bySession[ep.SessionID] = append(a.bySession[ep.SessionID], ep)
		if ep.RootSessionID != "" && ep.RootSessionID != ep.SessionID {
			a.bySession[ep.RootSessionID] = append(a.bySession[ep.RootSessionID], ep)
		}
		if ep.IsOpen() {
			root := ep.RootSessionID
			if root == "" {
				root = ep.SessionID
			}
			if open[ep.WorkItemID] == nil {
				open[ep.WorkItemID] = map[string]bool{}
			}
			open[ep.WorkItemID][root] = true
		}
	}
	for id, roots := range open {
		a.openHolders[id] = len(roots)
	}
	titles := map[string]string{}
	if !wantTitles {
		return a, titles, nil
	}
	if nodes, err := graph.LoadAll(wipnoteDir); err == nil {
		for _, n := range nodes {
			titles[n.ID] = n.Title
		}
	}
	return a, titles, nil
}

func (a *claimAttribution) hasSession(id string) bool { return len(a.bySession[id]) > 0 }

// attribute splits one session's cost across the work items whose claim
// episodes cover each request. A request covered by N distinct items is split
// evenly; a request covered by none (and all unpriced requests) lands in
// "unattributed". The sum over buckets always equals the session total.
func (a *claimAttribution) attribute(s sessionTelemetry, get func(string) *costBucket) {
	eps := a.bySession[s.SessionID]
	var attributed costBucket
	for _, p := range s.points {
		items := map[string]bool{}
		for _, ep := range eps {
			if !p.ts.Before(ep.StartedAt) && (ep.EndedAt.IsZero() || !p.ts.After(ep.EndedAt)) {
				items[ep.WorkItemID] = true
			}
		}
		if len(items) == 0 {
			continue
		}
		share := p.b
		n := float64(len(items))
		share.CostUSD /= n
		share.TokensIn = int64(float64(share.TokensIn) / n)
		share.TokensOut = int64(float64(share.TokensOut) / n)
		share.Requests = 0 // a split request is counted once, below
		for id := range items {
			get(id).add(share)
		}
		attributed.add(costBucket{CostUSD: p.b.CostUSD, Requests: 1, TokensIn: p.b.TokensIn, TokensOut: p.b.TokensOut})
		// Credit the request count to the lexically first item so totals add up.
		first := ""
		for id := range items {
			if first == "" || id < first {
				first = id
			}
		}
		get(first).Requests++
	}
	rest := costBucket{
		CostUSD: s.CostUSD - attributed.CostUSD, Requests: s.APIRequests - attributed.Requests,
		TokensIn: s.TokensIn - attributed.TokensIn, TokensOut: s.TokensOut - attributed.TokensOut,
		Unpriced: s.UnpricedRequests,
	}
	if rest.CostUSD < 1e-9 {
		rest.CostUSD = 0
	}
	if rest.Requests > 0 || rest.CostUSD > 0 {
		get(unattributedKey).add(rest)
	}
}

// ---- wipnote_work_items ----------------------------------------------------

type workItemsInput struct {
	Status     string `json:"status,omitempty" jsonschema:"filter: todo | in-progress | blocked | done | ..."`
	Type       string `json:"type,omitempty" jsonschema:"filter: feature | bug | spike | track | plan"`
	StaleDays  int    `json:"stale_days,omitempty" jsonschema:"only items not updated for at least this many days (oldest first)"`
	Limit      int    `json:"limit,omitempty" jsonschema:"max items returned (default 25, max 100)"`
	SinceHours int    `json:"since_hours,omitempty" jsonschema:"cost window in hours (default 168, max 720)"`
}

type workItemRow struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Priority    string  `json:"priority,omitempty"`
	Title       string  `json:"title" jsonschema:"untrusted; control-stripped and truncated to 80 runes"`
	UpdatedAt   string  `json:"updated_at,omitempty"`
	AgeDays     int     `json:"age_days" jsonschema:"whole days since last update"`
	OpenHolders int     `json:"open_claim_holders" jsonschema:"distinct root sessions with an open claim episode"`
	Episodes    int     `json:"claim_episodes"`
	CostUSD     float64 `json:"cost_usd" jsonschema:"cost attributed from claim-episode intervals inside the window"`
}

type workItemsResult struct {
	toolMeta
	Items           []workItemRow `json:"items"`
	ItemsMatched    int           `json:"items_matched"`
	ItemsReturned   int           `json:"items_returned"`
	ItemsTruncated  bool          `json:"items_truncated"`
	ItemsTotal      int           `json:"items_total" jsonschema:"all work items in the project before filtering"`
	CostAvailable   bool          `json:"cost_available" jsonschema:"false when no telemetry exists in the window; cost_usd is then 0"`
	UnattributedUSD float64       `json:"unattributed_usd" jsonschema:"in-window cost not covered by any claim episode"`
}

func buildWorkItems(wipnoteDir string, in workItemsInput, now time.Time) (workItemsResult, error) {
	start := time.Now()
	hours := clampInt(in.SinceHours, defaultCostHours, maxSinceHours)
	limit := clampInt(in.Limit, defaultToolLimit, maxToolLimit)
	since := now.Add(-time.Duration(hours) * time.Hour)

	nodes, err := graph.LoadAll(wipnoteDir)
	if err != nil {
		return workItemsResult{}, fmt.Errorf("load work items: %w", err)
	}
	attr, _, err := loadClaimAttribution(wipnoteDir, false)
	if err != nil {
		return workItemsResult{}, err
	}
	scan, err := scanTelemetryOpts(wipnoteDir, since, scanOptions{pointsFor: attr.hasSession})
	if err != nil {
		return workItemsResult{}, err
	}
	costs := map[string]*costBucket{}
	for _, s := range scan.Sessions {
		attr.attribute(s, func(k string) *costBucket {
			if costs[k] == nil {
				costs[k] = &costBucket{}
			}
			return costs[k]
		})
	}

	res := workItemsResult{ItemsTotal: len(nodes), CostAvailable: scan.Available}
	if b := costs[unattributedKey]; b != nil {
		res.UnattributedUSD = roundUSD(b.CostUSD)
	}
	var rows []workItemRow
	for _, n := range nodes {
		if in.Status != "" && string(n.Status) != in.Status {
			continue
		}
		if in.Type != "" && n.Type != in.Type {
			continue
		}
		age := 0
		if !n.UpdatedAt.IsZero() {
			age = int(now.Sub(n.UpdatedAt).Hours() / 24)
		}
		if in.StaleDays > 0 && age < in.StaleDays {
			continue
		}
		row := workItemRow{
			ID: n.ID, Type: n.Type, Status: string(n.Status), Priority: string(n.Priority),
			Title: cleanTitle(n.Title), AgeDays: age,
			OpenHolders: attr.openHolders[n.ID], Episodes: attr.episodes[n.ID],
		}
		if !n.UpdatedAt.IsZero() {
			row.UpdatedAt = n.UpdatedAt.UTC().Format(time.RFC3339)
		}
		if b := costs[n.ID]; b != nil {
			row.CostUSD = roundUSD(b.CostUSD)
		}
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if in.StaleDays > 0 {
			if a.AgeDays != b.AgeDays {
				return a.AgeDays > b.AgeDays
			}
		} else if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		} else if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID < b.ID
	})
	res.ItemsMatched = len(rows)
	if len(rows) > limit {
		rows, res.ItemsTruncated = rows[:limit], true
	}
	res.Items = append([]workItemRow{}, rows...)
	res.ItemsReturned = len(res.Items)
	res.toolMeta = newToolMeta(now, hours, since, scan, start)
	return res, nil
}
