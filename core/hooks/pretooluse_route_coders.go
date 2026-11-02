package hooks

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// A wipnote coder subagent, by the name the harness gives a plugin's agent.
var wipnoteCoder = regexp.MustCompile(`^wipnote:[a-z-]+-coder$`)

// guardRouteCoders denies a wipnote coder subagent inside herdr or the Herdr app
// and names wipnote delegate spawn, because a lean worker costs fewer tokens and
// prompt wording alone did not change which one sessions chose (feat-2d0fabbf).
// Outside Herdr, spawn cannot run, so nothing changes there. Other subagents,
// such as Explore or wipnote:researcher, are left alone.
func guardRouteCoders(st *preToolState) *guardOutcome {
	return routeCoders(st.event, os.Getenv)
}

func routeCoders(event *CloudEvent, getenv func(string) string) *guardOutcome {
	if event == nil || (event.ToolName != "Agent" && event.ToolName != "Task") {
		return nil
	}
	if getenv("WIPNOTE_NATIVE_CODERS") == "1" || !insideHerdr(getenv) {
		return nil
	}
	agent, _ := event.ToolInput["subagent_type"].(string)
	if !wipnoteCoder.MatchString(agent) {
		return nil
	}
	return block(fmt.Sprintf(`Inside Herdr, coding work goes to a lean worker instead of %s. Run:
  wipnote delegate spawn NAME --harness claude --work-item ID --brief-file PATH
then wipnote delegate wait NAME --timeout 10m, wipnote delegate result NAME and wipnote delegate stop NAME.
Choose its model with --profile simple, standard or deep, and add --worktree when workers edit overlapping files.
A worker does not load this project's rules by default, so put any rule that must hold in its brief.
If spawn refuses, for example at the cap of 6 workers, do the work in this session. wipnote skill show delegate has the details.`, agent))
}

// insideHerdr is the check delegate spawn makes (herdr.EnvFrom), which core
// cannot import: the herdr TUI, or a herdr-core terminal.
func insideHerdr(getenv func(string) string) bool {
	return getenv("HERDR_ENV") == "1" || strings.TrimSpace(getenv("HC_TERMINAL_ID")) != ""
}
