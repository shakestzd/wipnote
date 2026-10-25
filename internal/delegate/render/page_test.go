package render

import (
	"reflect"
	"strings"
	"testing"
)

// run.html escapes everything taken from the run (raw HTML and Markdown in
// prose, titles, the label, the status and the log path) and opens on the
// answer only when the run succeeded (the mod's runpane.test.tsx case).
func TestPage_EscapesHTML(t *testing.T) {
	evil := `<script>alert(1)</script> "x" & <img src=x onerror=1>`
	html := Page(PageInput{Label: evil, Status: evil, OK: false, Answer: evil, LogPath: "/d/<x>",
		Parts: []Part{{Kind: PartProse, Text: evil}, {Kind: PartFold, Title: evil, Body: "```\n" + evil + "\n```", Failed: true}}})
	for _, bad := range []string{"<script>alert", "<img", "/d/<x>"} {
		if strings.Contains(html, bad) {
			t.Errorf("run.html holds raw %q", bad)
		}
	}
	if n := strings.Count(html, "<script>"); n != 1 {
		t.Errorf("want only the page's own script, found %d", n)
	}
	if !strings.Contains(html, `data-first="live"`) || !strings.Contains(html, `<details class="failed" open>`) {
		t.Error("a failed run should open on Live with its failed fold open")
	}
	ok := Page(PageInput{Label: "l", Status: "s", OK: true, Answer: "a", LogPath: "/d"})
	if !strings.Contains(ok, `data-first="answer"`) {
		t.Error("a run that succeeded with an answer should open on the answer")
	}
	// Markdown in prose is rendered, never passed through as HTML
	prose := MDToHTML("# <b>h</b>\n\n**<i>x</i>** `<u>` [l](javascript:alert(1))")
	if strings.Contains(prose, "<b>") || strings.Contains(prose, "<i>") || strings.Contains(prose, "<u>") || strings.Contains(prose, "<a") {
		t.Errorf("prose HTML leaked: %s", prose)
	}
}

// The mod's mdToHtml cases: emphasis outside code spans only, the pane's
// fence rule.
func TestPage_MDToHTML(t *testing.T) {
	cases := map[string]string{
		"**b** `c` *i*\n\n- one\n\n> q\n\n```\n<x>\n```": "<p><strong>b</strong> <code>c</code> <em>i</em></p>\n<ul><li>one</li></ul>\n<blockquote>q</blockquote>\n<pre>&lt;x&gt;</pre>",
		"run `glob **/*.go` and `a*b*c`":                 "<p>run <code>glob **/*.go</code> and <code>a*b*c</code></p>",
		"```js``` is the tag\n\nnext":                    "<p><code>js</code> is the tag</p>\n<p>next</p>",
		"``a `b` c`` d":                                  "<p><code>a `b` c</code> d</p>",
		"````\n```\nin\n````\nout":                       "<pre>```\nin</pre>\n<p>out</p>",
		"### Turn\n\n### Assistant\n\nhi":                "<h3>Assistant</h3>\n<p>hi</p>",
		"``a`":                                           "<p>`<code>a</code></p>",
	}
	for in, want := range cases {
		if got := MDToHTML(in); got != want {
			t.Errorf("MDToHTML(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// parseRun: prose and folds, markers in fences are text, an unclosed fold
// waits until the run is final.
func TestPage_ParseRun(t *testing.T) {
	src := "### A\n\nhi\n\n<!-- fold closed: T · exit 0 · 1 line -->\n**Bash** `t`\n\n```\n<!-- /fold -->\n```\n<!-- /fold -->\n\n```\n<!-- fold closed: fake -->\n```\n"
	parts := ParseRun(src, false)
	var kinds []PartKind
	for _, p := range parts {
		kinds = append(kinds, p.Kind)
	}
	if !reflect.DeepEqual(kinds, []PartKind{PartProse, PartFold, PartProse}) {
		t.Fatalf("kinds = %v", kinds)
	}
	if parts[1].Body != "```\n<!-- /fold -->\n```" {
		t.Errorf("fold body = %q", parts[1].Body)
	}
	open := "<!-- fold closed: U · exit 0 · 1 line -->\n```\nz\n```\n"
	if got := ParseRun(open, false); len(got) != 0 {
		t.Errorf("an unclosed fold should wait: %v", got)
	}
	if got := ParseRun(open, true); len(got) != 1 || got[0].Kind != PartFold {
		t.Errorf("final: %v", got)
	}
	if got := ParseRun("one\ntw", false); len(got) != 1 || got[0].Text != "one" {
		t.Errorf("a partial last line should wait: %v", got)
	}
	cut := ParseRun("<!-- fold closed: Bash: a... · exit 0 · 1 line -->\n**Bash** `a b`\n<!-- /fold -->\n", false)
	if cut[0].Body != "**Bash** `a b`" {
		t.Errorf("a cut title keeps its label: %q", cut[0].Body)
	}
}

// The status line says how the run ended, as the mod's pane does.
func TestPage_Status(t *testing.T) {
	cases := []struct {
		in   StatusInput
		want string
	}{
		{StatusInput{Running: true, Seconds: 5}, "running for 5s"},
		{StatusInput{Exit: 0, Seconds: 82, Usage: "input 5 (cached 0), output 1"}, "finished in 1m22s · input 5 (cached 0), output 1"},
		{StatusInput{Exit: 2, Seconds: 3}, "failed with exit 2 in 3s"},
	}
	for _, c := range cases {
		if got := Status(c.in); got != c.want {
			t.Errorf("Status(%+v) = %q, want %q", c.in, got, c.want)
		}
	}
}
