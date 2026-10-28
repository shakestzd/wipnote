package main

import (
	"slices"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
)

// The delegations section of lineage (plan-28b4a7f6 slice 13). It reads the
// canonical delegations ledger and the claim ledger, never live run files, and
// replaces the orch sessions section: orch history reaches the ledger through
// `wipnote delegate import-orch` (slice 18).
//
// Link rules, kept from the retired orchlink: a row tagged with the root is
// asserted ("tag"); an untagged row whose worker sessions include one of the
// root's own implemented_in sessions is derived and labelled ("sid"); a row
// tagged with another item is never derived onto this one.

// Links as lineage shows them.
const (
	delegationLinkTag = "tag"
	delegationLinkSID = "sid"
)

// lineageDelegation is one delegations[] item in `wipnote lineage --json`.
type lineageDelegation struct {
	ID             string         `json:"id"`
	Mode           string         `json:"mode"`
	Harness        string         `json:"harness"`
	Name           string         `json:"name"`
	Link           string         `json:"link"`
	State          string         `json:"state"`
	WorkItem       string         `json:"work_item,omitempty"`
	CallerSession  string         `json:"caller_session,omitempty"`
	WorkerSessions []string       `json:"worker_sessions"`
	StartedAt      string         `json:"started_at,omitempty"`
	EndedAt        string         `json:"ended_at,omitempty"`
	Claims         []lineageClaim `json:"claims,omitempty"`
}

// lineageClaim is one claim episode held by a delegation's worker session.
// CoClaim names the dlg- id when the caller held the same item at the same
// time: under q-claim-handoff that is one delegation's co-claim, not two
// claimants colliding.
type lineageClaim struct {
	Episode   string `json:"episode"`
	WorkItem  string `json:"work_item"`
	Session   string `json:"session"`
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at,omitempty"`
	Outcome   string `json:"outcome"` // a claim outcome, or "open"
	CoClaim   string `json:"co_claim,omitempty"`
}

// lineageDelegationsView is the section: the rows, or why the ledger could
// not be read (so an unreadable ledger never reads as "no delegations").
type lineageDelegationsView struct {
	Rows []lineageDelegation
	Err  string
}

// isDelegatedItemKind reports whether a delegation can serve the root: the
// ledger's work_item column holds feat-, bug- and spk- ids only.
func isDelegatedItemKind(kind lineageKind) bool {
	switch kind {
	case kindFeature, kindBug, kindSpike:
		return true
	default:
		return false
	}
}

// loadLineageDelegations returns the delegations serving a work item, with
// their worker claim episodes. nil when the root is not a work item or no
// delegation serves it.
func loadLineageDelegations(wipnoteDir, root string, kind lineageKind, forward []lineageNode) *lineageDelegationsView {
	if !isDelegatedItemKind(kind) {
		return nil
	}
	recs, err := delegationledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return &lineageDelegationsView{Err: err.Error()}
	}
	rows := delegationsForWorkItem(recs, root, directSessions(root, forward))
	if len(rows) == 0 {
		return nil
	}
	attachClaims(wipnoteDir, recs, rows)
	return &lineageDelegationsView{Rows: rows}
}

// delegationsForWorkItem applies the link rules to the ledger rows.
func delegationsForWorkItem(recs []delegationledger.Record, root string, sessions map[string]bool) []lineageDelegation {
	var out []lineageDelegation
	for _, r := range recs {
		switch {
		case r.WorkItem == root:
			out = append(out, toLineageDelegation(r, delegationLinkTag))
		case r.WorkItem == "" && anySession(r.WorkerSessions, sessions):
			out = append(out, toLineageDelegation(r, delegationLinkSID))
		}
	}
	return out
}

func anySession(ids []string, set map[string]bool) bool {
	for _, id := range ids {
		if set[id] {
			return true
		}
	}
	return false
}

func toLineageDelegation(r delegationledger.Record, link string) lineageDelegation {
	state := "ended"
	if r.IsOpen() {
		state = "running"
	}
	workers := append([]string{}, r.WorkerSessions...)
	return lineageDelegation{
		ID: r.ID, Mode: r.Mode, Harness: r.Harness, Name: r.Name, Link: link, State: state,
		WorkItem: r.WorkItem, CallerSession: r.CallerSession, WorkerSessions: workers,
		StartedAt: jsonTime(r.Started), EndedAt: jsonTime(r.Ended),
	}
}

// directSessions returns the ids of the sessions the root was implemented in
// (its own implemented_in edges, not those of related items).
func directSessions(root string, forward []lineageNode) map[string]bool {
	out := map[string]bool{}
	for _, n := range forward {
		if n.Parent == root && n.EdgeType == "implemented_in" {
			out[n.ID] = true
		}
	}
	return out
}

// attachClaims joins each row's worker sessions to the claim ledger, read
// once. No claim data is copied into the delegations ledger. A claim ledger
// that cannot be read leaves the rows without claims: the section is advisory.
func attachClaims(wipnoteDir string, recs []delegationledger.Record, rows []lineageDelegation) {
	eps, err := claimledger.NewStore(wipnoteDir).ReadAll()
	if err != nil || len(eps) == 0 {
		return
	}
	byID := map[string]delegationledger.Record{}
	for _, r := range recs {
		byID[r.ID] = r
	}
	for i := range rows {
		rows[i].Claims = claimsFor(byID[rows[i].ID], eps)
	}
}

// claimsFor returns the episodes held by run's worker sessions, oldest first,
// each marked as a co-claim when a caller episode on the same item overlaps.
func claimsFor(run delegationledger.Record, eps []claimledger.Episode) []lineageClaim {
	var out []lineageClaim
	for _, e := range eps {
		if !slices.Contains(run.WorkerSessions, e.SessionID) {
			continue
		}
		c := lineageClaim{
			Episode: e.ID, WorkItem: e.WorkItemID, Session: e.SessionID,
			StartedAt: jsonTime(e.StartedAt), EndedAt: jsonTime(e.EndedAt), Outcome: string(e.Outcome),
		}
		if e.IsOpen() {
			c.Outcome = "open"
		}
		if callerOverlaps(run.CallerSession, e, eps) {
			c.CoClaim = run.ID
		}
		out = append(out, c)
	}
	return out
}

// callerOverlaps reports whether the caller held w's work item while w was open.
func callerOverlaps(caller string, w claimledger.Episode, eps []claimledger.Episode) bool {
	if caller == "" {
		return false
	}
	for _, c := range eps {
		if c.ID == w.ID || c.WorkItemID != w.WorkItemID || (c.SessionID != caller && c.RootSessionID != caller) {
			continue
		}
		if c.StartedAt.Before(endOrForever(w)) && w.StartedAt.Before(endOrForever(c)) {
			return true
		}
	}
	return false
}

// endOrForever treats an open episode as reaching past any recorded time.
func endOrForever(e claimledger.Episode) time.Time {
	if e.IsOpen() {
		return time.Unix(1<<62, 0)
	}
	return e.EndedAt
}
