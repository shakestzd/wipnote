package herdr

import (
	"context"
	"encoding/json"
	"testing"
)

// An agent list entry carries the pane cwd and, when herdr can resolve it,
// the foreground process's cwd (herdr CLI reference, agent list).
func TestWireAgent_CwdAndForegroundCwd(t *testing.T) {
	for _, tc := range []struct{ raw, cwd, fg string }{
		{`{"pane_id":"w1:p2","agent":"claude","agent_status":"blocked","cwd":"/work/a","foreground_cwd":"/work/b"}`, "/work/a", "/work/b"},
		{`{"pane_id":"w1:p2","agent":"claude","agent_status":"blocked","cwd":"/work/a"}`, "/work/a", ""},
		{`{"pane_id":"w1:p2","agent":"claude","agent_status":"blocked"}`, "", ""},
	} {
		var w wireAgent
		if err := json.Unmarshal([]byte(tc.raw), &w); err != nil {
			t.Fatal(err)
		}
		if a := w.agent(); a.Cwd != tc.cwd || a.ForegroundCwd != tc.fg || a.State != StateBlocked {
			t.Fatalf("%s -> %+v, want cwd %q foreground %q", tc.raw, a, tc.cwd, tc.fg)
		}
	}
}

// The fake's agent_list shape carries cwd and, when scripted, foreground_cwd,
// and the CLI reads both.
func TestAgentList_FakeCarriesCwds(t *testing.T) {
	installFakeScript(t, `{"rules":[{"match":["agent","list"],"replies":[{"shape":{"as":"agent_list","agents":[`+
		`{"pane_id":"w1:p2","cwd":"/work/a","foreground_cwd":"/work/b"},{"pane_id":"w1:p3","cwd":"/work/c"}]}}]}]}`)
	agents, err := New(Env{InHerdr: true}).AgentList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(agents) != 2 || agents[0].Cwd != "/work/a" || agents[0].ForegroundCwd != "/work/b" ||
		agents[1].Cwd != "/work/c" || agents[1].ForegroundCwd != "" {
		t.Fatalf("agents = %+v", agents)
	}
}
