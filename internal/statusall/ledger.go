package statusall

import (
	"sort"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// stateClaimed is a ledger-only agent's state: the claims ledger says it
// holds a work item, and nothing reports what it is doing right now.
const stateClaimed = "claimed"

// readSessions loads the sessions ledger into lc.
func readSessions(p *Project, lc *local) {
	lc.ended, lc.harness = map[string]bool{}, map[string]string{}
	recs, err := sessionledger.NewStore(lc.dir).ReadAll()
	if err != nil {
		p.Errors = append(p.Errors, "sessions: "+err.Error())
		return
	}
	for _, r := range recs {
		lc.ended[r.SessionID] = !r.IsOpen()
		lc.harness[r.SessionID] = r.Harness
	}
}

// ledgerAgents lists the open claim episodes whose root session has not
// ended: the agents the ledgers know hold a work item. A session missing
// from the sessions ledger counts as live, the rule claim reconcile uses.
func ledgerAgents(p *Project, lc local) []Agent {
	eps, err := claimledger.NewStore(lc.dir).ReadAll()
	if err != nil {
		p.Errors = append(p.Errors, "claims: "+err.Error())
		return nil
	}
	var out []Agent
	for _, e := range eps {
		if !e.IsOpen() || lc.ended[e.RootSessionID] {
			continue
		}
		a := Agent{Source: "ledger", Harness: normHarness(lc.harness[e.RootSessionID]), State: stateClaimed,
			WorkItem: e.WorkItemID, Session: e.SessionID, Since: stamp(e.StartedAt),
			LastLine: sessionLastLine(lc.runs, e.SessionID)}
		if n := lc.items[e.WorkItemID]; n != nil {
			a.Title = n.Title
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Since > out[j].Since })
	return out
}

// sessionLastLine is the last output line of the session's newest run that
// has one: the closest thing the ledgers have to what the agent last did.
func sessionLastLine(runs []runledger.Record, session string) string {
	var best time.Time
	line := ""
	for _, r := range runs {
		t := r.EndedAt
		if t.IsZero() {
			t = r.StartedAt
		}
		if r.Session == session && r.LastLine != "" && !t.Before(best) {
			best, line = t, r.LastLine
		}
	}
	return line
}

// normHarness maps a harness or agent kind to the ledger's tag (claude,
// codex, ...); an unknown kind is kept as given.
func normHarness(h string) string {
	if t := sessionledger.HarnessFromAgent(h); t != "" {
		return t
	}
	return h
}
