package session

import (
	"strings"
	"testing"
)

const exitMenu = "   shell · until [ -s x ]\n\n   ❯ 1. Exit and stop tasks\n     2. Move to background and exit\n     3. Stay\n\n   Enter to confirm · Esc to cancel\n"

func TestExitTasksMenu(t *testing.T) {
	if !ExitTasksMenu(exitMenu) {
		t.Fatal("menu with the cursor on option 1 not recognised")
	}
	for name, screen := range map[string]string{
		"cursor on stay":    strings.Replace(strings.Replace(exitMenu, "❯ 1.", "  1.", 1), "  3. Stay", "❯ 3. Stay", 1),
		"cursor on move":    strings.Replace(strings.Replace(exitMenu, "❯ 1.", "  1.", 1), "  2. Move", "❯ 2. Move", 1),
		"no menu":           "❯ \n",
		"only the one line": "❯ 1. Exit and stop tasks\n",
	} {
		if ExitTasksMenu(screen) {
			t.Fatalf("%s: read as the exit menu", name)
		}
	}
}

// A worker with background tasks answers /exit with Claude Code's exit menu;
// stop picks "Exit and stop tasks" (already selected) so the finished pane
// closes (bug-440acb23).
func TestStop_AnswersTheExitTasksMenu(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1")
	e.setAlive(4242, "")
	enters := 0
	e.herdr.onEnter = func() {
		enters++
		p := e.herdr.pane(PaneOf(s))
		if enters == 1 {
			p.screen = exitMenu
			return
		}
		p.fg = nil
	}
	res := stop(t, e, "w1")
	if !res.PaneClosed || enters != 2 {
		t.Fatalf("exit menu not answered: enters=%d %+v calls=%v", enters, res, e.herdr.Calls())
	}
}

// A second stop that finds the menu already up (the first stop timed out)
// answers it instead of reading it as a draft.
func TestStop_MenuAlreadyUpIsNotADraft(t *testing.T) {
	e := newEnv(t)
	s := runningWorker(t, e, "w1")
	e.setAlive(4242, "")
	p := e.herdr.pane(PaneOf(s))
	p.screen, p.styled = exitMenu, exitMenu
	e.herdr.onEnter = func() { p.fg = nil }
	res := stop(t, e, "w1")
	if !res.PaneClosed {
		t.Fatalf("menu read as a draft: %+v calls=%v", res, e.herdr.Calls())
	}
	for _, c := range e.herdr.Calls() {
		if strings.HasPrefix(c, "send-text") {
			t.Fatalf("typed into the menu: %v", e.herdr.Calls())
		}
	}
}
