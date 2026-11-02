package detect

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Upstream's manuals (hc --skill, herdr --skill) teach an agent to start a
// helper with `hc run --bg claude ...` or `herdr agent start ... --kind codex`.
// A helper started that way has no ledger row, run folder or claim, so the
// work disappears from lineage with no signal (feat-e21f4236). A template such
// as `hc run reviewer "..."` is left alone: its command lives in a file the
// hook does not read.

// hcValueFlags are the `hc run` flags that take a value, so the word after one
// is not the program.
var hcValueFlags = map[string]bool{
	"--name": true, "--cwd": true, "--on": true, "--label": true, "--parent": true, "--timeout": true,
}

const agentLaunchReasonText = `wipnote: %s starts a %s agent outside the delegations ledger, so it gets no run folder or claim and drops out of lineage. Start it through wipnote delegate, which uses the same terminal runtime:

  wipnote delegate spawn --harness %s --brief "<the task>"

Then use wipnote delegate send, wait, result and stop.`

// agentLaunchReason is the deny reason for a standalone command that starts
// claude or codex through hc or herdr, or "".
func agentLaunchReason(command string) string {
	if !strings.Contains(command, "claude") && !strings.Contains(command, "codex") {
		return ""
	}
	if !singleCommand(command) {
		return ""
	}
	words, ok := splitWords(command)
	if !ok {
		return ""
	}
	for len(words) > 0 && assignRe.MatchString(words[0].text) {
		words = words[1:]
	}
	if len(words) < 2 {
		return ""
	}
	args := texts(words)
	var via, harness string
	switch base := filepath.Base(args[0]); {
	case (base == "hc" || base == "herdr-core") && args[1] == "run":
		via, harness = "hc run", hcRunProgram(args[2:])
	case base == "herdr" && len(args) > 2 && args[1] == "agent" && args[2] == "start":
		via, harness = "herdr agent start", herdrAgentKind(args[3:])
	}
	if harness != "claude" && harness != "codex" {
		return ""
	}
	return fmt.Sprintf(agentLaunchReasonText, via, harness, harness)
}

// hcRunProgram is the program name `hc run` would start: the first word after
// its flags, or the first word of a single quoted shell line.
func hcRunProgram(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			if i+1 < len(args) {
				return programName(args[i+1])
			}
			return ""
		case hcValueFlags[a]:
			i++
		case strings.HasPrefix(a, "-"):
		default:
			return programName(a)
		}
	}
	return ""
}

func programName(word string) string {
	fields := strings.Fields(word)
	if len(fields) == 0 {
		return ""
	}
	return filepath.Base(fields[0])
}

// herdrAgentKind reads --kind from `herdr agent start`'s arguments.
func herdrAgentKind(args []string) string {
	for i, a := range args {
		if a == "--" {
			return ""
		}
		if a == "--kind" && i+1 < len(args) {
			return args[i+1]
		}
		if kind, ok := strings.CutPrefix(a, "--kind="); ok {
			return kind
		}
	}
	return ""
}
