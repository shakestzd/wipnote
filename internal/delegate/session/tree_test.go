package session

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func TestTree_CallerRunWorkerSubagents(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	other := e.opts("w2")
	other.CallerSession = "sess-other"
	other.CallerPane = "w1:p9"
	if _, err := Spawn(t.Context(), e.deps, other); err != nil {
		t.Fatal(err)
	}
	e.event(s.Run, "sid-one", runstore.KindSessionStart)
	e.event(s.Run, "sid-one", runstore.KindSubagentStart, func(ev *runstore.Event) { ev.AgentID, ev.AgentType = "ag-1", "Explore" })
	e.event(s.Run, "sid-one", runstore.KindSubagentStop, func(ev *runstore.Event) { ev.AgentID = "ag-1" })
	// Other side (D4): an event that only carries an agent id is no sub-agent.
	e.event(s.Run, "sid-one", runstore.KindToolStart, func(ev *runstore.Event) { ev.AgentID = "ag-noise" })

	nodes, err := Tree(e.deps, "sess-caller")
	if err != nil || len(nodes) != 1 || nodes[0].Session != "sess-caller" || len(nodes[0].Runs) != 1 {
		t.Fatalf("nodes %+v err %v", nodes, err)
	}
	run := nodes[0].Runs[0]
	if run.Run != s.Run || run.Name != "w1" || len(run.Workers) != 1 || run.Workers[0].Session != "sid-one" {
		t.Fatalf("run %+v", run)
	}
	subs := run.Workers[0].SubAgents
	if len(subs) != 1 || subs[0].ID != "ag-1" || subs[0].Type != "Explore" || subs[0].Open {
		t.Fatalf("subagents %+v", subs)
	}
	var text bytes.Buffer
	RenderTree(&text, nodes)
	for _, want := range []string{"sess-caller", "w1", s.Run, "sid-one", "ag-1 (Explore)"} {
		if !strings.Contains(text.String(), want) {
			t.Fatalf("text tree lacks %q:\n%s", want, text.String())
		}
	}
	if strings.Contains(text.String(), "ag-noise") || strings.Contains(text.String(), "sess-other") {
		t.Fatalf("tree shows another caller or a noise agent:\n%s", text.String())
	}
	all, _ := Tree(e.deps, "")
	if len(all) != 2 {
		t.Fatalf("all callers: %+v", all)
	}
	if _, err := json.Marshal(all); err != nil {
		t.Fatal(err)
	}
}

func TestSpawn_LiveClaudeWorker(t *testing.T) {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("WIPNOTE_DELEGATE_LIVE") != "1" {
		t.Skip("live herdr check: run inside herdr with WIPNOTE_DELEGATE_LIVE=1 (Mac check)")
	}
	t.Skip("live check is driven by the Phase B script on a real herdr")
}
