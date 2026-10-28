package session

import (
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// A tool_end clears only the ask of the same tool call: with tools running
// side by side, an unrelated tool finishing leaves a pending permission
// listed (PR 296 review).
func TestQuestions_UnrelatedToolEndKeepsAsk(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindPermissionRequest, func(ev *runstore.Event) {
		ev.Tool, ev.ToolUseID, ev.Summary = "Bash", "pending", "rm -rf build"
	})
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "Read", "other" })
	if qs := e.questions("w1"); len(qs) != 1 || qs[0].ID != "pending" {
		t.Fatalf("unrelated tool_end cleared the ask: %+v", qs)
	}
	if st := e.state("w1"); st != StateQuestion {
		t.Fatalf("status %s", st)
	}
	// Other side: the ask's own tool_end clears it.
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "Bash", "pending" })
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("own tool_end left the ask: %+v", qs)
	}
}

// An ask with no tool_use_id cannot be matched, so it keeps the earlier
// rule: the next tool_end clears it.
func TestQuestions_UnidentifiedAskClearsOnNextToolEnd(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindPermissionRequest, func(ev *runstore.Event) { ev.Tool, ev.Summary = "Bash", "ls" })
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "Bash", "tu-1" })
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("%+v", qs)
	}
}
