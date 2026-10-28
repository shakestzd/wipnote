package herdr

import (
	"encoding/json"
	"testing"
)

// An agent list entry carries its working folder when herdr reports one, and
// none when it does not (the documented shape has no cwd).
func TestWireAgent_CwdIsOptional(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{"pane_id":"w1:p2","agent":"claude","agent_status":"blocked","cwd":"/work/proj"}`, "/work/proj"},
		{`{"pane_id":"w1:p2","agent":"claude","agent_status":"blocked"}`, ""},
	} {
		var w wireAgent
		if err := json.Unmarshal([]byte(tc.raw), &w); err != nil {
			t.Fatal(err)
		}
		if a := w.agent(); a.Cwd != tc.want || a.State != StateBlocked {
			t.Fatalf("%s -> %+v, want cwd %q", tc.raw, a, tc.want)
		}
	}
}
