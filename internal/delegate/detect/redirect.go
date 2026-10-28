package detect

import (
	"os"
	"path/filepath"

	"fmt"
	"github.com/shakestzd/wipnote/internal/delegate/container"
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
// the --container form instead (decision C, slice 21).
//
// It redirects only what delegate run takes: a command delegate run would
// refuse (chained, fed on stdin, behind dev exec, another --output-format)
// runs as typed. A command that starts with wipnote delegate is never
// redirected, so the retry cannot loop.
func Redirect(command string) string { return RedirectAt(command, "") }

// RedirectAt is Redirect for a command run in cwd (the hook event's; the
// process's when empty). cwd decides, for a ccx task, whether the project
// has a devcontainer --container can use.
func RedirectAt(command, cwd string) string {
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
	return reason(command, words, r, cwd)
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

const ccxReasonText = `wipnote: a bare ccx task skips the delegations ledger, the run folder and the herdr pane. ccx is container Claude, which wipnote delegate runs in the project's own devcontainer:

  %s

wipnote delegate run --container takes the task's arguments unchanged after -- and runs them as claude -p there.`

// reason builds the deny text. words start at the tool; what comes before it
// (VAR=value words) stays in front of wipnote.
func reason(command string, words []word, r oneshot.Recipe, cwd string) string {
	cmd := strings.TrimRight(command, " \t\r\n")
	at := words[0].start
	env := strings.TrimLeft(cmd[:at], " \t\r\n")
	same := env + "wipnote delegate run -- " + cmd[at:]
	if r.Kind != oneshot.KindCcx {
		return fmt.Sprintf(reasonText, r.Name(), same)
	}
	if !containerForm(words[2:], cwd) {
		// --container would not run this as ccx meant it: name the form
		// that does.
		return fmt.Sprintf(reasonText, r.Name(), same)
	}
	container := env + "wipnote delegate run --container -- " + cmd[words[2].start:]
	return fmt.Sprintf(ccxReasonText, container)
}

// containerForm reports whether a ccx task's arguments can be offered as
// `delegate run --container`: plain task words only (a ccx flag such as
// --resume means something to ccx, not to claude -p), that --container
// accepts, in a project whose own devcontainer it can use.
func containerForm(args []word, cwd string) bool {
	if len(args) == 0 {
		return false
	}
	for _, w := range args {
		if strings.HasPrefix(w.text, "-") {
			return false
		}
	}
	if _, err := oneshot.ClassifyContainer(texts(args)); err != nil {
		return false
	}
	root := projectRoot(cwd)
	return root != "" && container.Check(root) == nil
}

// projectRoot is the folder holding .wipnote/ at or above cwd, or "".
func projectRoot(cwd string) string {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	for dir := filepath.Clean(cwd); dir != "" && filepath.IsAbs(dir); dir = filepath.Dir(dir) {
		if fi, err := os.Stat(filepath.Join(dir, ".wipnote")); err == nil && fi.IsDir() {
			return dir
		}
		if dir == filepath.Dir(dir) {
			break
		}
	}
	return ""
}
