package session

import "strings"

// trustDialogs are the folder-trust dialogs, matched by BOTH of their labels
// (Phase B runs 1-3, Claude Code 2.1.310 and codex-cli 0.164.0). Claude's
// default row is "No, exit", so a bare enter or a digit declines trust and
// the worker quits: delegate never presses a key on either dialog.
var trustDialogs = []struct {
	harness string
	labels  [2]string
}{
	{HarnessClaude, [2]string{"No, exit", "Yes, I trust this folder"}},
	{HarnessCodex, [2]string{"1. Trust and continue", "2. Quit"}},
}

// loginScreens are a harness's first-run sign-in screens. A worker there never
// binds a session, and without this wait reports only a timeout (Codex 0.166.0).
var loginScreens = []struct {
	harness string
	labels  [2]string
}{
	{HarnessCodex, [2]string{"Welcome to Codex", "Sign in with ChatGPT"}},
}

// TrustDialog names the folder-trust dialog or sign-in screen on screen, or
// returns "".
func TrustDialog(screen string) string {
	for _, d := range trustDialogs {
		if strings.Contains(screen, d.labels[0]) && strings.Contains(screen, d.labels[1]) {
			return d.harness + ` folder-trust dialog ("` + d.labels[0] + `" / "` + d.labels[1] + `")`
		}
	}
	for _, d := range loginScreens {
		if strings.Contains(screen, d.labels[0]) && strings.Contains(screen, d.labels[1]) {
			return d.harness + " is not signed in: run `" + d.harness + " login`, or sign in in the pane"
		}
	}
	return ""
}
