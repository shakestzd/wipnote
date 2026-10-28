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
			f.turn, f.question, open = TurnNone, false, map[string]bool{}
		case runstore.KindPrompt:
			f.turn, f.question = TurnRunning, false
		case runstore.KindStop:
			f.turn, f.question = TurnEnded, false
		case runstore.KindQuestion, runstore.KindPermissionRequest:
			f.question = true
		case runstore.KindToolStart:
			if isShellTool(ev.Tool) && ev.ToolUseID != "" {
				open[ev.ToolUseID] = true
			}
		case runstore.KindToolEnd:
			delete(open, ev.ToolUseID)
			f.question = false
		}
	}
	f.shell = len(open) > 0
	return f
}

func isShellTool(name string) bool {
	switch name {
	case "Bash", "shell", "exec_command", "local_shell":
		return true
	}
	return false
}

// taskFacts reports whether the newest inbox task has its result file.
func taskFacts(store *runstore.Store, name string) (pending, answered bool) {
	mb, err := store.Mailbox(name)
	if err != nil {
		return false, false
	}
	msgs, err := mb.Read(runstore.Inbox)
	if err != nil {
		return false, false
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Type == runstore.TypeTask {
			_, ok, _ := mb.Result(msgs[i].ID)
			return !ok, ok
		}
	}
	return false, false
}

// observe builds the Observation for one worker. Herdr is asked only
// whether the pane lives, whether it holds the pinned terminal, and, while no
// session is bound, for its screen (a trust dialog). It never decides that a
// worker finished from the screen.
func observe(ctx context.Context, d Deps, rf *runstore.RunFolder, info Info) (Observation, eventFacts) {
	evs, _ := rf.ReadEvents()
	f := factsOf(evs)
	o := Observation{Bound: len(f.sessions) > 0, Turn: f.turn, Question: f.question, ShellRunning: f.shell}
	o.TaskPending, o.TaskAnswered = taskFacts(d.Store, info.Name)
	if d.Herdr == nil || info.Pane == "" {
		return o, f
	}
	pane := herdr.PaneID(info.Pane)
	p, err := d.Herdr.Get(ctx, pane)
	switch {
	case herdr.IsAbsent(err):
		o.PaneGone, o.GoneWhy = true, fmt.Sprintf("pane %s is gone", pane)
	case err == nil && info.TerminalID != "" && p.TerminalID != "" && p.TerminalID != info.TerminalID:
		o.PaneGone, o.GoneWhy = true, fmt.Sprintf("pane %s now holds terminal %s, not %s", pane, p.TerminalID, info.TerminalID)
	case err == nil && !o.Bound:
		if screen, rerr := d.Herdr.PaneRead(ctx, pane, screenLines); rerr == nil {
			o.Dialog = TrustDialog(screen)
		}
	}
	return o, f
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
