package session

import (
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func askEvent(kind, id, text string) func(*runstore.Event) {
	return func(ev *runstore.Event) {
		ev.Kind, ev.ToolUseID, ev.Tool, ev.Summary = kind, id, "AskUserQuestion", text
	}
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
	if hook.ID != "tu-1" || hook.Text != "Which store?" || !hook.Answered || !hook.Pane {
		t.Fatalf("hook ask %+v", hook)
	}
	if file.ID != task+"-q1" || !file.Answered || file.Answer != "yes" || file.Recommended != "dev" || len(file.Options) != 2 {
		t.Fatalf("file ask %+v", file)
	}
}

// A hook ask keeps its options; one the worker's pane answered (its tool use
// ended) is answered with the pane's text and never reported as open; a
// failed tool use leaves it open; a multi-question call is one ask each.
func TestRunAsks_HookOptionsAndPaneAnswers(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	opts := []runstore.EventAsk{{Question: "Which store?", Header: "Store", Options: []string{"SQLite", "HTML"}}}
	e.event(s.Run, "sid-1", runstore.KindQuestion, func(ev *runstore.Event) {
		askEvent(runstore.KindQuestion, "tu-1", "Which store? [SQLite | HTML]")(ev)
		ev.Asks = opts
	})
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) {
		ev.ToolUseID, ev.Asks = "tu-1", []runstore.EventAsk{{Question: "Which store?", Answer: "HTML"}}
	})
	two := []runstore.EventAsk{{Question: "A?", Options: []string{"x", "y"}}, {Question: "B?", MultiSelect: true}}
	e.event(s.Run, "sid-1", runstore.KindQuestion, func(ev *runstore.Event) {
		askEvent(runstore.KindQuestion, "tu-2", "A?; B?")(ev)
		ev.Asks = two
	})
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.ToolUseID, ev.Summary = "tu-2", "failed" })
	e.event(s.Run, "sid-1", runstore.KindQuestion, askEvent(runstore.KindQuestion, "tu-3", "Old event?"))
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.ToolUseID = "tu-3" })

	asks, err := RunAsks(e.deps, "w1")
	if err != nil || len(asks.Asks) != 4 {
		t.Fatalf("%+v %v", asks, err)
	}
	one, a, b, old := asks.Asks[0], asks.Asks[1], asks.Asks[2], asks.Asks[3]
	if one.ID != "tu-1" || one.Text != "Which store?" || one.Header != "Store" || len(one.Options) != 2 || !one.Answered || !one.Pane || one.Answer != "HTML" {
		t.Fatalf("pane-answered ask %+v", one)
	}
	if a.ID != "tu-2#1" || b.ID != "tu-2#2" || a.Answered || b.Answered || !b.MultiSelect || len(a.Options) != 2 {
		t.Fatalf("failed multi-question asks %+v %+v", a, b)
	}
	if old.ID != "tu-3" || old.Text != "Old event?" || !old.Answered || !old.Pane || old.Answer != "" {
		t.Fatalf("summary-only answered ask %+v", old)
	}
}
