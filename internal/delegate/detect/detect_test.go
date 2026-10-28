package detect

import "testing"

// The recogniser tables below are the retired herdr pane mod's, case for case
// (its hooks/register.test.ts and claude.test.ts), so the Go port keeps the
// mod's behaviour. The mod's "already wrapped" marker is gone with the mod:
// nothing wraps a command any more, so a comment no longer hides a run.

// TestDetect_ModCases ports the mod's "recognises" and "standalone" tables.
func TestDetect_ModCases(t *testing.T) {
	cases := []struct {
		name string
		want Tool
		cmds []string
	}{
		{"codex yes", Codex, []string{
			`codex exec "do it"`,
			`cd repo && codex exec -s workspace-write "do it"`,
			`FOO=1 codex e "do it"`,
			`codex review --base main`,
			`x=$(codex exec "q")`,
			`a; codex exec "q"`,
			"# a comment\ncodex exec \"q\"",
			"# codex-" + "herdr-pane\ncodex exec \"the old marker hides nothing\"",
		}},
		{"codex no", None, []string{
			`codex "interactive prompt"`,
			`codex --help`,
			`codex exec --help`,
			`codex review -h`,
			`codex --version`,
			`wipnote codex --resume abc`,
			`echo "run codex exec later"`,
			`echo 'a; codex exec b'`,
			`git log --grep="codex exec"`,
			"cat <<EOF\ncodex exec hidden\nEOF",
			`ls codex-exec-notes.md`,
		}},
		{"ccx yes", Ccx, []string{
			`ccx task "x"`,
			`~/tools/claude-container/ccx task "x"`,
			`ccx task "x" --resume abc`,
			`cd repo && /opt/bin/ccx task "x"`,
			"# a comment\nccx task \"x\"",
		}},
		{"ccx no", None, []string{
			`ccx status`, `ccx up`, `ccx down`, `ccx login`,
			`echo "ccx task"`,
			"cat <<EOF\nccx task hidden\nEOF",
			`ccx task --help`,
			`ls ccx-task-notes.md`,
		}},
		{"claude yes", Claude, []string{
			`claude -p "say ok" --allowedTools Read`, `claude --print "x"`, `cd r && claude --model opus -p "x"`,
			`dev exec claude -p "x"`, `dev exec -d svc/api claude -p "/code-review high a.ts" --allowedTools "Read,Grep,Glob"`,
			`dev exec -d=app /usr/local/bin/claude -p "x"`, `claude -p "x" | tail -3`, `claude -p "list sizes" | sort -h`, "claude \\\n  -p \"x\"",
		}},
		{"claude no", None, []string{
			`claude`, `claude "fix it"`, `claude --version`, `claude -p --version`, `claude -p "x" -h`, `claude --help`,
			`claude mcp list -p`, `claude plugin install -p x`, `dev exec -d svc claude`, `echo "claude -p x"`, `echo 'a; claude -p b'`,
			"cat <<EOF\nclaude -p hidden\nEOF", `wipnote claude -p x`, `claude-p x`,
			`/usr/local/bin/claude -p "x"`, `~/.local/bin/claude -p "x"`,
			`claude -p "x" --output-format json`, `claude -p --output-format=text "x"`, "claude \\\n  -p \"x\" --output-format json",
			"cd x && claude\nmkdir -p out", "claude --resume abc\nls -p",
		}},
		{"codex keeps its rule: an info flag anywhere", None, []string{`codex exec "x" | sort -h`}},
	}
	for _, tc := range cases {
		for _, c := range tc.cmds {
			if got := Find(c); got != tc.want {
				t.Errorf("%s: Find(%q) = %q, want %q", tc.name, c, got, tc.want)
			}
		}
	}
	for _, c := range []string{`codex exec "x"`, `ccx task "x"`} {
		if Find(c) == Claude {
			t.Errorf("Find(%q) = claude", c)
		}
	}
}

// TestDetect_ModStandaloneCases ports the mod's standalone/chained tables.
func TestDetect_ModStandaloneCases(t *testing.T) {
	standalone := []string{
		`codex exec "x"`, `codex exec "a; b | c" 2>&1`, `FOO=1 codex e "x" > out.txt`, `ccx task "x" --resume abc`,
		"codex exec - <<EOF\nbody; x | y\nEOF",
		`claude -p "x"`, `dev exec -d d claude -p "x"`, `claude -p "x" > out.md`,
	}
	chained := []string{
		`codex exec "x" 2>&1 | tail -6`, `codex exec "x"; echo done`, `a && codex exec "x"`, `codex exec "x" || true`,
		"codex exec \"x\"\necho after", `echo $(codex exec "x")`, `codex exec "x" &`, `ccx task "x" | head -1`,
		`claude -p "x" | tail -1`, `a; dev exec claude -p "x"`,
		`echo hi`, // not a delegation
	}
	for _, c := range standalone {
		if !Standalone(c) {
			t.Errorf("Standalone(%q) = false, want true", c)
		}
	}
	for _, c := range chained {
		if Standalone(c) {
			t.Errorf("Standalone(%q) = true, want false", c)
		}
	}
}

// TestDetect_BlanksQuotesAndHeredocs: quoted text and heredoc bodies hold
// prompts, not commands, so they are blanked before the search; an
// unterminated heredoc or quote is left as it is.
func TestDetect_BlanksQuotesAndHeredocs(t *testing.T) {
	cases := []struct{ in, want string }{
		{`echo "codex exec x"`, `echo ""`},
		{`echo 'a; codex exec b'`, `echo ""`},
		{`echo "a \"q\" b" c`, `echo "" c`},
		{"cat <<EOF\ncodex exec hidden\nEOF", "cat "},
		{"cat <<'EOF' > f\nx; y\nEOF\nls", "cat \nls"},
		{"cat <<-\"END\"\n\tbody\n\tEND\nls", "cat \nls"},
		{"cat <<EOF\nno terminator", "cat <<EOF\nno terminator"},
		{`echo "unterminated`, `echo "unterminated`},
		{"a <<<word", "a <<<word"},
	}
	for _, tc := range cases {
		if got := Blank(tc.in); got != tc.want {
			t.Errorf("Blank(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// Two-sided: the same words outside quotes are a delegation.
	if Find(`echo x; codex exec y`) != Codex {
		t.Error("an unquoted codex exec after ; must be found")
	}
	if Find(`echo "x; codex exec y"`) != None {
		t.Error("a quoted codex exec must not be found")
	}
}

// TestDetect_InfoFlagsAreNotRuns: --help, -h, --version and -V ask for text,
// not a run. codex and ccx look anywhere in the command; claude looks only at
// its own words, so a later `sort -h` still leaves a claude run.
func TestDetect_InfoFlagsAreNotRuns(t *testing.T) {
	for _, c := range []string{
		`codex exec --help`, `codex review -h`, `codex e -V`, `codex exec --version`,
		`ccx task --help`, `ccx task -h`,
		`claude -p --version`, `claude -p "x" -h`, `claude --print -V`,
		`codex exec "x" | sort -h`,
	} {
		if got := Find(c); got != None {
			t.Errorf("Find(%q) = %q, want none", c, got)
		}
	}
	for c, want := range map[string]Tool{
		`codex exec "x"`:                   Codex,
		`codex exec "explain --help"`:      Codex,
		`claude -p "list sizes" | sort -h`: Claude,
		`ccx task "what does -h do"`:       Ccx,
	} {
		if got := Find(c); got != want {
			t.Errorf("Find(%q) = %q, want %q", c, got, want)
		}
	}
}
