package session

import (
	"context"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// exitMenuPick matches Claude Code's exit menu with its cursor on option 1.
// A worker that still has background tasks answers /exit with that menu
// (1. Exit and stop tasks / 2. Move to background and exit / 3. Stay).
var exitMenuPick = regexp.MustCompile(`(?m)^[\s│]*❯\s*1\.\s*Exit and stop tasks`)

// ExitTasksMenu reports that screen shows Claude Code's exit menu with
// "Exit and stop tasks" selected. Stop answers it with Enter: stopping a
// worker means its tasks end too, and the menu's lines are not a person's
// draft (bug-440acb23). The cursor must be on option 1, so Enter never
// picks "Move to background" or "Stay".
func ExitTasksMenu(screen string) bool {
	return strings.Contains(screen, "Move to background and exit") && exitMenuPick.MatchString(screen)
}

// answerExitMenu presses Enter when pane shows the exit menu with option 1
// selected, and reports whether it did.
func answerExitMenu(ctx context.Context, c herdr.Client, pane herdr.PaneID) bool {
	screen, err := c.PaneRead(ctx, pane, 12)
	if err != nil || !ExitTasksMenu(screen) {
		return false
	}
	return c.SendKeys(ctx, pane, "enter") == nil
}
