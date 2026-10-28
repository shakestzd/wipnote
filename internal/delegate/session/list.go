package session

import (
	"context"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// Row is one run as list prints it.
type Row struct {
	Run      string   `json:"run"`
	Mode     string   `json:"mode"`
	Name     string   `json:"name,omitempty"`
	Harness  string   `json:"harness"`
	WorkItem string   `json:"work_item,omitempty"`
	Pane     string   `json:"pane,omitempty"`
	State    State    `json:"state"`
	Detail   string   `json:"detail,omitempty"`
	Sessions []string `json:"sessions,omitempty"`
	Open     bool     `json:"open"`
}

// One-shot and ended runs carry these states.
const (
	StateRunning State = "running"
	StateEnded   State = "ended"
)

// List returns the open runs (all runs with all) with readiness. For each
// open interactive run it also adopts a late agent (rename and pin) and moves
// bound sessions into the ledger's worker_sessions (Enrich). A run whose pane
// is gone goes through the worker-loss path: its row closes as gone and its
// sessions' claim episodes close as expired.
func List(ctx context.Context, d Deps, all bool) ([]Row, error) {
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, r := range recs {
		if !r.IsOpen() && !all {
			continue
		}
		rows = append(rows, listRow(ctx, d, r))
	}
	return rows, nil
}

func listRow(ctx context.Context, d Deps, r delegationledger.Record) Row {
	row := Row{Run: r.ID, Mode: r.Mode, Name: r.Name, Harness: r.Harness, WorkItem: r.WorkItem,
		Sessions: r.WorkerSessions, Open: r.IsOpen()}
	switch {
	case !r.IsOpen():
		row.State, row.Detail = StateEnded, r.EndReason
		return row
	case r.Mode != delegationledger.ModeInteractive:
		row.State = StateRunning
		return row
	}
	rf, info, err := loadInfo(d.Store, r.ID)
	if err != nil {
		row.State, row.Detail = StateGone, err.Error()
		return row
	}
	row.Pane = info.Pane
	if adopt(ctx, d.Herdr, &info) {
		_ = WriteInfo(rf, info)
	}
	o, f := observe(ctx, d, rf, info)
	rd := Ready(o)
	row.State, row.Detail = rd.State, rd.Detail
	if len(f.sessions) > 0 {
		if _, err := d.Ledger.Enrich(r.ID, delegationledger.Enrichment{WorkerSessions: f.sessions}); err == nil {
			row.Sessions = mergeSessions(r.WorkerSessions, f.sessions)
			r.WorkerSessions = row.Sessions
		}
	}
	if o.PaneGone {
		loseWorker(d, r, rf, info)
		row.Open = false
	}
	return row
}

func mergeSessions(have, add []string) []string {
	out := append([]string(nil), have...)
	seen := map[string]bool{}
	for _, s := range have {
		seen[s] = true
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
