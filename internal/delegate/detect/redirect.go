package detect

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
)

// delegateRunRe is the loop guard: a command that already goes through
// wipnote delegate, behind optional VAR=value words or a path.
var delegateRunRe = regexp.MustCompile(`^\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*(?:\S*/)?wipnote\s+delegate(?:\s|$)`)

var assignRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// Redirect is the PreToolUse deny reason for a bare standalone delegation, or
// "" to let the command run as typed (q-intercept: deny-redirect). The reason
// names the exact `wipnote delegate run -- <same argv>`, so the retry is the
// same call and passes the normal permission checks; a bare ccx task is shown
// the --container form instead (decision C).
//
// It redirects only what delegate run takes: a command delegate run would
// refuse (chained, fed on stdin, behind dev exec, another --output-format)
// runs as typed. A command that starts with wipnote delegate is never
// redirected, so the retry cannot loop.
func Redirect(command string) string {
	if !mentionsTool(command) || delegateRunRe.MatchString(command) {
		return ""
	}
	if !Standalone(command) {
		return ""
	}
	words, ok := splitWords(command)
	if !ok {
		return ""
	}
	for len(words) > 0 && assignRe.MatchString(words[0].text) {
		words = words[1:]
	}
	r, err := oneshot.Classify(texts(words))
	if err != nil {
		return ""
	}
	return reason(command, words, r)
}

// mentionsTool is the fast path: a command that names no delegated tool is
// not looked at further.
func mentionsTool(command string) bool {
	return strings.Contains(command, "codex") || strings.Contains(command, "ccx") || strings.Contains(command, "claude")
}

func texts(words []word) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w.text
	}
	return out
}

const reasonText = `wipnote: a bare %s call skips the delegations ledger, the run folder and the herdr pane. Run the same command through wipnote delegate:

  %s

wipnote delegate run takes the tool's argv unchanged after --.`

const ccxReasonText = `wipnote: a bare ccx task skips the delegations ledger, the run folder and the herdr pane. ccx is container Claude, which wipnote delegate runs as:

  %s

--container is coming and this wipnote does not have it yet. Until it does, run:

  %s

wipnote delegate run takes the tool's argv unchanged after --.`

// reason builds the deny text. words start at the tool; what comes before it
// (VAR=value words) stays in front of wipnote.
func reason(command string, words []word, r oneshot.Recipe) string {
	cmd := strings.TrimRight(command, " \t\r\n")
	at := words[0].start
	env := strings.TrimLeft(cmd[:at], " \t\r\n")
	same := env + "wipnote delegate run -- " + cmd[at:]
	if r.Kind != oneshot.KindCcx {
		return fmt.Sprintf(reasonText, r.Name(), same)
	}
	args := ""
	if len(words) > 2 {
		args = cmd[words[2].start:]
	}
	container := strings.TrimRight(env+"wipnote delegate run --container -- "+args, " ")
	return fmt.Sprintf(ccxReasonText, container, same)
}
