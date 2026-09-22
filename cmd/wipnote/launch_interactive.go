package main

import (
	"fmt"
	"os"
	"strings"
)

// allowNonTTYEnv is the environment opt-in equivalent of
// --allow-non-interactive: set to 1/true/yes to let `wipnote claude` /
// `wipnote codex` start an interactive harness even though stdin is not a
// terminal (e.g. a deliberately scripted launch under expect or a PTY-less CI
// wrapper that supplies its own input).
const allowNonTTYEnv = "WIPNOTE_ALLOW_NON_TTY"

const allowNonInteractiveFlag = "allow-non-interactive"

// launchStdinIsTTYFn is the test seam for the interactive-launch guard.
var launchStdinIsTTYFn = func() bool { return isInteractiveTerminalFile(os.Stdin) }

// errNonInteractiveLaunch is returned by requireInteractiveLaunch. Callers get
// it BEFORE any launch side effect (launch marker, serve autostart, collector
// spawn, worktree creation, chooser, feature start) so a refused launch leaves
// no session or attribution state behind (issue #148).
type errNonInteractiveLaunch struct {
	harness string
}

func (e *errNonInteractiveLaunch) Error() string {
	return fmt.Sprintf(
		"wipnote %s: refusing to launch an interactive %s session because stdin is not a terminal.\n"+
			"This usually means `wipnote %s` was run from a script, a nested agent task, or another\n"+
			"harness's tool call, where an interactive TUI cannot run and its session bootstrap\n"+
			"would steal work-item attribution from the calling session.\n"+
			"  - To initialise wipnote for this project without launching, use the wipnote CLI\n"+
			"    directly (e.g. `wipnote status`, `wipnote who`, `wipnote <type> start <id>`).\n"+
			"  - To run %s headless, pass its own non-interactive flags after the wipnote flags\n"+
			"    (%s).\n"+
			"  - To force an interactive launch anyway, pass --%s or set %s=1.",
		e.harness, e.harness, e.harness, e.harness, headlessHint(e.harness), allowNonInteractiveFlag, allowNonTTYEnv)
}

func headlessHint(harness string) string {
	if harness == harnessCodex {
		return "`wipnote codex exec \"<prompt>\"`"
	}
	return "`wipnote claude -p \"<prompt>\"`"
}

// nonTTYAllowedByEnv reports whether WIPNOTE_ALLOW_NON_TTY opts in.
func nonTTYAllowedByEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(allowNonTTYEnv))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// extraArgsRequestHeadless reports whether the pass-through harness args
// already select a non-interactive mode, in which case a non-TTY stdin is the
// expected way to run: Claude Code's `-p`/`--print` and Codex's `exec`
// subcommand. Those launches keep working unchanged.
func extraArgsRequestHeadless(harness string, extraArgs []string) bool {
	for i, a := range extraArgs {
		switch harness {
		case harnessCodex:
			if i == 0 && a == "exec" {
				return true
			}
		default:
			if a == "-p" || a == "--print" || strings.HasPrefix(a, "--print=") {
				return true
			}
		}
	}
	return false
}

// requireInteractiveLaunch refuses an interactive nested launch when stdin is
// not a terminal, unless the caller opted in via --allow-non-interactive /
// WIPNOTE_ALLOW_NON_TTY or the pass-through args already request a headless
// mode. It must run before any session or attribution write.
func requireInteractiveLaunch(harness string, allowFlag bool, extraArgs []string) error {
	if allowFlag || nonTTYAllowedByEnv() || extraArgsRequestHeadless(harness, extraArgs) {
		return nil
	}
	if launchStdinIsTTYFn() {
		return nil
	}
	return &errNonInteractiveLaunch{harness: harness}
}
