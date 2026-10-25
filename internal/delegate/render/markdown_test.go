package render

import (
	"regexp"
	"strings"
	"testing"
)

// A fence is always longer than any backtick run inside, at least three,
// and nothing inside can close it: untrusted output cannot break out.
func TestMarkdown_SafeFence(t *testing.T) {
	hostile := "```\n`````\n# not a heading\n<script>alert(1)</script>\n~~~\n    ```"
	got := fenced(hostile, "")
	if want := "``````\n" + hostile + "\n``````"; got != want {
		t.Fatalf("fenced = %q, want %q", got, want)
	}
	for n, want := range map[int]int{0: 3, 2: 3, 3: 4, 7: 8} {
		body := strings.Repeat("`", n) + "x"
		f := fenced(body, "text")
		if !strings.HasPrefix(f, strings.Repeat("`", want)+"text\n") || !strings.HasSuffix(f, "\n"+strings.Repeat("`", want)) {
			t.Errorf("run of %d: fence %q", n, f)
		}
	}
	// Inline code: the delimiter outgrows the inner run; edge backticks get
	// a space so the span still closes.
	for in, want := range map[string]string{
		"ls -la":      "`ls -la`",
		"echo `date`": "`` echo `date` ``",
		"a ``` b":     "````a ``` b````",
		"x\n  y":      "`x y`",
	} {
		if got := code(in); got != want {
			t.Errorf("code(%q) = %q, want %q", in, got, want)
		}
	}
	// Prose closes a fence it leaves open, and a fold marker outside a fence
	// is escaped so assistant text cannot make one.
	if got := prose("before\n```py\nunclosed"); got != "before\n```py\nunclosed\n```" {
		t.Errorf("prose did not close the fence: %q", got)
	}
	p := prose("<!-- fold closed: x -->\n```\n<!-- /fold -->\n```")
	if !strings.HasPrefix(p, `\<!-- fold closed: x -->`) || !strings.Contains(p, "```\n<!-- /fold -->\n```") {
		t.Errorf("prose fold markers: %q", p)
	}
	if got := esc("bad *thing* # [x](y) <b>"); got != `bad \*thing\* \# \[x\](y) \<b\>` {
		t.Errorf("esc = %q", got)
	}
}

// A fold title is one line that cannot end its HTML comment early.
func TestMarkdown_FoldTitle(t *testing.T) {
	f := fold("echo '-->' x\nline2 \x1b[31mred", true, "body")
	first := strings.SplitN(f, "\n", 2)[0]
	if strings.Count(first, "-->") != 1 || !strings.HasSuffix(first, " -->") || !strings.HasPrefix(first, "<!-- fold open: ") {
		t.Errorf("fold title line %q", first)
	}
	if regexp.MustCompile("\x1b").MatchString(first) {
		t.Errorf("ANSI escape left in title %q", first)
	}
}

// The sh -c wrapper is stripped only when the outer shell would not expand
// anything; otherwise the command is kept as written.
func TestMarkdown_UnwrapShell(t *testing.T) {
	for in, want := range map[string]string{
		"/bin/zsh -lc 'head -n 1 AGENTS.md'":         "head -n 1 AGENTS.md",
		`/bin/zsh -lc "echo \$HOME \` + "`date\\`\"": "echo $HOME `date`",
		`sh -c "echo hi"`:                            "echo hi",
		"bash script.sh arg":                         "bash script.sh arg",
		`/bin/zsh -lc "echo $HOME"`:                  `/bin/zsh -lc "echo $HOME"`,
		"/bin/zsh -lc 'echo hi":                      "/bin/zsh -lc 'echo hi",
		"/bin/zsh -lc ls;rm":                         "/bin/zsh -lc ls;rm",
	} {
		if got := unwrapShell(in); got != want {
			t.Errorf("unwrapShell(%q) = %q, want %q", in, got, want)
		}
	}
}

// joinfence (the one fixes.test.ts case that is a render rule): neighbouring
// text fences, one per plain line of codex review, run together; other
// blocks keep their closing fence.
func TestMarkdown_JoinsNeighbouringTextFences(t *testing.T) {
	in := "```text\na\n```\n\n```text\nb\n```\n\n```\ncode\n```\n\n```text\nc\n```\n"
	want := "```text\na\nb\n```\n\n```\ncode\n```\n\n```text\nc\n```\n"
	var j joinFence
	var out strings.Builder
	for _, l := range splitLines([]byte(in)) {
		out.WriteString(j.line(l))
	}
	out.WriteString(j.flush())
	if out.String() != want {
		t.Errorf("joinfence:\n got %q\nwant %q", out.String(), want)
	}
}
