package detect

import (
	"strings"
	"testing"
)

// TestRedirect_NamesTheSameArgv: the reason holds the exact command behind
// `wipnote delegate run --`, so the retry is the same call through the ledger.
func TestRedirect_NamesTheSameArgv(t *testing.T) {
	cases := map[string]string{
		`codex exec -s workspace-write "implement feat-1"`: `wipnote delegate run -- codex exec -s workspace-write "implement feat-1"`,
		`codex e 'q'`:                            `wipnote delegate run -- codex e 'q'`,
		`codex review --base main`:               `wipnote delegate run -- codex review --base main`,
		`claude -p "say ok" --allowedTools Read`: `wipnote delegate run -- claude -p "say ok" --allowedTools Read`,
		"claude \\\n  -p \"x\"":                  "wipnote delegate run -- claude \\\n  -p \"x\"",
		`FOO=1 codex e "x"`:                      `FOO=1 wipnote delegate run -- codex e "x"`,
		"  codex exec \"x\"\n":                   `wipnote delegate run -- codex exec "x"`,
	}
	for cmd, want := range cases {
		got := Redirect(cmd)
		if !strings.Contains(got, "\n  "+want+"\n") {
			t.Errorf("Redirect(%q) = %q\nwant it to hold the line %q", cmd, got, want)
		}
	}
}

// TestRedirect_CcxTaskGetsContainerForm: ccx means container Claude, so a bare
// ccx task is shown the --container form with the task's own arguments, and
// only that form now that --container exists (slice 21).
func TestRedirect_CcxTaskGetsContainerForm(t *testing.T) {
	got := Redirect(`ccx task "fix the build" --resume abc`)
	if want := "\n  wipnote delegate run --container -- \"fix the build\" --resume abc\n"; !strings.Contains(got, want) {
		t.Errorf("ccx reason %q\nwant it to hold %q", got, want)
	}
	if strings.Contains(got, "-- ccx task") {
		t.Errorf("ccx reason still offers the exact-argv form: %q", got)
	}
	if strings.Contains(got, "-- codex") || strings.Contains(got, "-- claude") {
		t.Errorf("ccx reason names another tool: %q", got)
	}
}

// TestRedirect_LoopGuard: a command that already goes through wipnote
// delegate is never redirected, whatever it holds.
func TestRedirect_LoopGuard(t *testing.T) {
	for _, c := range []string{
		`wipnote delegate run -- codex exec "x"`,
		`wipnote delegate run --label L -- claude -p "x"`,
		`FOO=1 wipnote delegate run -- ccx task "x"`,
		`~/.local/bin/wipnote delegate run -- codex review`,
		Redirect(`codex exec "x"`), // the reason itself, pasted back
	} {
		if got := Redirect(c); got != "" {
			t.Errorf("Redirect(%q) = %q, want no redirect", c, got)
		}
	}
}

// TestRedirect_PassesWhatDelegateRunWouldRefuse: anything delegate run cannot
// take as one argv runs as typed: chained commands (v2, q-chained), stdin
// (delegate run does not forward it), dev exec, non-stream-json output, info
// flags and plain commands.
func TestRedirect_PassesWhatDelegateRunWouldRefuse(t *testing.T) {
	for _, c := range []string{
		`codex exec "x" | tail -6`, `a && codex exec "x"`, `codex exec "x"; echo done`, `echo $(codex exec "x")`,
		"codex exec - <<EOF\nbody\nEOF", `codex exec - < prompt.md`,
		`dev exec claude -p "x"`, `claude -p "x" --output-format json`,
		`codex exec --help`, `ls`, `git log --grep="codex exec"`, `echo "claude -p x"`,
		`codex exec "unterminated`,
		// The caller owns stdout: delegate run would print its report there,
		// not the tool's answer, so these run as typed.
		`claude -p "write the doc" > answer.md`, `codex exec "x" > out.txt 2>&1`,
		`codex review --base main >> review.md`, `claude -p "x" &> all.log`, `codex exec "x" 2>/dev/null`,
	} {
		if got := Redirect(c); got != "" {
			t.Errorf("Redirect(%q) = %q, want no redirect", c, got)
		}
	}
}
