package session

import (
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func askEvent(kind, id, text string) func(*runstore.Event) {
	return func(ev *runstore.Event) { ev.Kind, ev.ToolUseID, ev.Tool, ev.Summary = kind, id, "AskUserQuestion", text }
}

func TestRunAsks_AllAsksAnsweredOrNotPermissionsDropped(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindQuestion, askEvent(runstore.KindQuestion, "tu-1", "Which store?"))
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.ToolUseID = "tu-1" }) // answered in the pane
	e.event(s.Run, "sid-1", runstore.KindPermissionRequest, askEvent(runstore.KindPermissionRequest, "tu-2", "Run rm?"))
	task := e.task("w1")
	if _, err := Reply(e.deps, s.Run, ReplyOptions{Task: task, Body: "Which branch?", Ask: true, Options: []string{"main", "dev"}, Recommended: "dev"}); err != nil {
		t.Fatal(err)
	}
	e.answer("w1", task+"-q1")

	asks, err := RunAsks(e.deps, "w1")
	if err != nil {
		t.Fatal(err)
	}
	if asks.WorkItem != e.row(s.Run).WorkItem || len(asks.Asks) != 2 {
		t.Fatalf("%+v", asks)
	}
	hook, file := asks.Asks[0], asks.Asks[1]
	if hook.ID != "tu-1" || hook.Text != "Which store?" || hook.Answered {
		t.Fatalf("hook ask %+v", hook)
	}
	if file.ID != task+"-q1" || !file.Answered || file.Answer != "yes" || file.Recommended != "dev" || len(file.Options) != 2 {
		t.Fatalf("file ask %+v", file)
	}
}
