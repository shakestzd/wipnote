package session

import (
	"context"
	"fmt"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// screenLines is how much of a starting pane is read to find a trust dialog.
const screenLines = 40

// eventFacts is what a run's hook events say about its current session.
type eventFacts struct {
	sessions []string // bound session ids, oldest first
	turn     Turn
	question bool
	asks     []runstore.Event // the pending question and permission events
	shell    bool
}

// factsOf reads the merged events. A session_start (the bind) begins a new
// session: /clear gives a new id, and the turn state starts over.
func factsOf(evs []runstore.Event) eventFacts {
	var f eventFacts
	seen := map[string]bool{}
	open := map[string]bool{} // shell tool calls started, not ended
	for _, ev := range evs {
		switch ev.Kind {
		case runstore.KindSessionStart:
			if ev.SID != "" && !seen[ev.SID] {
				seen[ev.SID] = true
				f.sessions = append(f.sessions, ev.SID)
			}
			f.turn, f.asks, open = TurnNone, nil, map[string]bool{}
		case runstore.KindPrompt:
			f.turn, f.asks = TurnRunning, nil
		case runstore.KindStop:
			f.turn, f.asks = TurnEnded, nil
		case runstore.KindQuestion, runstore.KindPermissionRequest:
			f.asks = append(f.asks, ev)
		case runstore.KindToolStart:
			if isShellTool(ev.Tool) && ev.ToolUseID != "" {
				open[ev.ToolUseID] = true
			}
		case runstore.KindToolEnd:
			delete(open, ev.ToolUseID)
			f.asks = clearAsks(f.asks, ev.ToolUseID)
		}
	}
	f.question, f.shell = len(f.asks) > 0, len(open) > 0
	return f
}

// clearAsks drops the asks a tool_end answers: the one of the same tool
// call. Tools can run side by side, so an unrelated call's end leaves other
// asks pending. An ask with no tool_use_id cannot be matched and clears on
// any tool_end, as before.
func clearAsks(asks []runstore.Event, toolUseID string) []runstore.Event {
	var keep []runstore.Event
	for _, a := range asks {
		if a.ToolUseID != "" && a.ToolUseID != toolUseID {
			keep = append(keep, a)
		}
	}
	return keep
}

func isShellTool(name string) bool {
	switch name {
	case "Bash", "shell", "exec_command", "local_shell":
		return true
	}
	return false
}

// task is a worker's newest inbox task: its message, its turn (the count of
// tasks so far) and whether the worker acked it.
type task struct {
	mb    *runstore.Mailbox
	msg   runstore.Message
	turn  int
	acked bool
}

// newestTask returns run's newest inbox task in name's mailbox; ok is false
// when there is none. A task of an earlier run with the same name never
// counts: every reader (send, wait, list, result, questions, inbox) scopes
// the shared mailbox to its run this way.
func newestTask(store *runstore.Store, name, run string) (task, bool) {
	mb, err := store.Mailbox(name)
	if err != nil {
		return task{}, false
	}
	msgs, err := mb.Read(runstore.Inbox)
	if err != nil {
		return task{mb: mb}, false
	}
	t := task{mb: mb}
	for _, m := range msgs {
		if m.Type == runstore.TypeTask && m.Run == run {
			t.msg, t.turn = m, t.turn+1
		}
	}
	t.acked = t.turn > 0 && mb.Acked(t.msg.ID)
	return t, t.turn > 0
}

// taskFacts reports whether the newest inbox task has its result file.
func taskFacts(store *runstore.Store, name, run string) (pending, answered bool) {
	t, ok := newestTask(store, name, run)
	if !ok {
		return false, false
	}
	_, found, _ := t.mb.Result(t.msg.ID)
	return !found, found
}

// observe builds the Observation for one worker. Herdr is asked only
// whether the pane lives, whether it holds the pinned terminal, once a
// session is bound whether the harness still runs in it, and, while no
// session is bound, for its screen (a trust dialog). It never decides that a
// worker finished from the screen.
func observe(ctx context.Context, d Deps, rf *runstore.RunFolder, info Info) (Observation, eventFacts) {
	evs, _ := rf.ReadEvents()
	f := factsOf(evs)
	o := Observation{Bound: len(f.sessions) > 0, Turn: f.turn, Question: f.question, ShellRunning: f.shell}
	o.TaskPending, o.TaskAnswered = taskFacts(d.Store, info.Name, info.Run)
	o.Question = o.Question || len(pendingQFiles(d.Store, info.Name, info.Run)) > 0 // D7
	if d.Herdr == nil || info.Pane == "" {
		return o, f
	}
	pane := herdr.PaneID(info.Pane)
	p, err := d.Herdr.Get(ctx, pane)
	switch {
	case herdr.IsAbsent(err):
		o.PaneGone, o.GoneWhy = true, fmt.Sprintf("pane %s is gone", pane)
	case err == nil && reused(info, p):
		o.PaneGone, o.Reused = true, true
		o.GoneWhy = fmt.Sprintf("pane %s now holds terminal %s, not %s", pane, p.TerminalID, info.TerminalID)
	case err == nil && o.Bound:
		o.Exited, o.GoneWhy = exited(ctx, d, pane)
	case err == nil && !o.Bound:
		if screen, rerr := d.Herdr.PaneRead(ctx, pane, screenLines); rerr == nil {
			o.Dialog = TrustDialog(screen)
		}
	}
	return o, f
}

// exited reports a bound worker whose harness has quit while its pane lives
// on (a crash, or a person exited it): herdr positively sees the shell alone.
// An unknown foreground is never an exit.
func exited(ctx context.Context, d Deps, pane herdr.PaneID) (bool, string) {
	pi, err := d.Herdr.Processes(ctx, pane)
	if err != nil || !bareShell(pi) {
		return false, ""
	}
	return true, fmt.Sprintf("the worker exited; pane %s holds only its shell", pane)
}

// adoptAndSave adopts a late agent and saves only the adoption's fields, so
// a field another writer set since info was loaded (the boot's workspace
// folder) is kept; info is refreshed from what was written.
func adoptAndSave(ctx context.Context, d Deps, rf *runstore.RunFolder, info *Info) {
	if !adopt(ctx, d.Herdr, info) {
		return
	}
	term := info.TerminalID
	if saved, err := UpdateInfo(rf, func(i *Info) { i.TerminalID, i.Renamed = term, true }); err == nil {
		*info = saved
	}
}

// adopt renames and pins a late agent (orch launch.adopt_late_agent): once
// herdr lists an agent in the worker's pane, it is renamed to the worker's
// name and its terminal id is pinned. It reports whether info changed.
func adopt(ctx context.Context, c herdr.Client, info *Info) bool {
	if c == nil || info.Pane == "" || info.Renamed {
		return false
	}
	agents, err := c.AgentList(ctx)
	if err != nil {
		return false
	}
	for _, a := range agents {
		if a.Pane != herdr.PaneID(info.Pane) {
			continue
		}
		pin := herdr.PinOf(a)
		if err := c.AgentRename(ctx, pin, info.Name); err != nil {
			return false
		}
		info.TerminalID, info.Renamed = a.TerminalID, true
		return true
	}
	return false
}
