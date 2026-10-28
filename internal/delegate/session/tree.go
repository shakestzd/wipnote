package session

import (
	"fmt"
	"io"
	"sort"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// CallerNode is one caller session and the runs it started.
type CallerNode struct {
	Session string    `json:"session"`
	Runs    []RunNode `json:"runs"`
}

// RunNode is one run and the worker sessions it hosted.
type RunNode struct {
	Run      string       `json:"run"`
	Mode     string       `json:"mode"`
	Name     string       `json:"name,omitempty"`
	Harness  string       `json:"harness"`
	WorkItem string       `json:"work_item,omitempty"`
	Open     bool         `json:"open"`
	State    State        `json:"state"`
	Group    Group        `json:"group"`
	Workers  []WorkerNode `json:"workers"`
}

// WorkerNode is one worker session (a /clear starts another) and the
// sub-agents it ran (D4: only real subagent_start/stop pairs count).
type WorkerNode struct {
	Session   string              `json:"session"`
	SubAgents []runstore.SubAgent `json:"subagents"`
}

// noCaller groups runs started outside a session (a human shell).
const noCaller = "(no caller session)"

// Tree groups the ledger's runs by caller session. caller limits it to one
// caller; "" lists every caller. Worker sessions are the ledger's plus any
// bind events not yet enriched, in order.
func Tree(d Deps, caller string) ([]CallerNode, error) {
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return nil, err
	}
	index := map[string]int{}
	var nodes []CallerNode
	for _, r := range recs {
		c := r.CallerSession
		if c == "" {
			c = noCaller
		}
		if caller != "" && c != caller {
			continue
		}
		i, ok := index[c]
		if !ok {
			i = len(nodes)
			index[c] = i
			nodes = append(nodes, CallerNode{Session: c})
		}
		nodes[i].Runs = append(nodes[i].Runs, runNode(d.Store, r))
	}
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Session < nodes[j].Session })
	return nodes, nil
}

func runNode(store *runstore.Store, r delegationledger.Record) RunNode {
	n := RunNode{Run: r.ID, Mode: r.Mode, Name: r.Name, Harness: r.Harness, WorkItem: r.WorkItem, Open: r.IsOpen(), Workers: []WorkerNode{}}
	var evs []runstore.Event
	rf, err := store.Run(r.ID)
	if err == nil {
		evs, _ = rf.ReadEvents()
	}
	n.State = treeState(rf, r)
	n.Group, _ = GroupOf(n.State)
	sessions := mergeSessions(r.WorkerSessions, factsOf(evs).sessions)
	for _, sid := range sessions {
		var own []runstore.Event
		for _, ev := range evs {
			if ev.SID == sid {
				own = append(own, ev)
			}
		}
		subs := runstore.SubAgents(own)
		if subs == nil {
			subs = []runstore.SubAgent{}
		}
		n.Workers = append(n.Workers, WorkerNode{Session: sid, SubAgents: subs})
	}
	return n
}

// treeState is a run's state without observing it: an open worker's is the
// one list last kept in its folder (starting before any list), any other
// run's comes from its ledger row.
func treeState(rf *runstore.RunFolder, r delegationledger.Record) State {
	if !r.IsOpen() || r.Mode != delegationledger.ModeInteractive {
		return RunStateOf(r.IsOpen(), r.Exit, r.EndReason)
	}
	if rf != nil {
		if o, ok := ReadObserved(rf); ok {
			return o.State
		}
	}
	return StateStarting
}

// RenderTree writes nodes as an indented text tree.
func RenderTree(w io.Writer, nodes []CallerNode) {
	if len(nodes) == 0 {
		fmt.Fprintln(w, "no delegation runs")
		return
	}
	for _, c := range nodes {
		fmt.Fprintf(w, "caller %s\n", c.Session)
		for _, r := range c.Runs {
			state := "ended"
			if r.Open {
				state = "open"
			}
			label := r.Harness
			if r.Name != "" {
				label = r.Name + " (" + r.Harness + ")"
			}
			fmt.Fprintf(w, "  run %s %s %s %s %s · %s\n", r.Run, label, r.WorkItem, state, r.State, r.Group.Label())
			for _, wk := range r.Workers {
				fmt.Fprintf(w, "    session %s\n", wk.Session)
				for _, s := range wk.SubAgents {
					open := ""
					if s.Open {
						open = " running"
					}
					fmt.Fprintf(w, "      subagent %s (%s)%s\n", s.ID, s.Type, open)
				}
			}
		}
	}
}
