package render

import (
	"os"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// run styles lines in order with one state and returns each output.
func runStyle(lines []string, width int, color bool) []string {
	var st lineStyle
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = st.style(l, width, color)
	}
	return out
}

func one(l string) string { return runStyle([]string{l}, 0, true)[0] }

func stripped(s string) string { return ansi.Strip(s) }

func TestStyle_PassthroughWithoutColour(t *testing.T) {
	lines := []string{"### Assistant", "**b** *e* `c` ~~s~~", `\*x\*`, "```go", "x := 1", "```", "| a | b |", "|---|---|", "- item", "> q", ""}
	for i, got := range runStyle(lines, 10, false) {
		if got != lines[i] {
			t.Errorf("line %d changed: %q -> %q", i, lines[i], got)
		}
	}
}

func TestStyle_Heading(t *testing.T) {
	got := one("### Assistant")
	if stripped(got) != "Assistant" || !strings.Contains(got, "\x1b[") {
		t.Errorf("heading = %q", got)
	}
	if !strings.Contains(got, "1") { // bold is SGR 1
		t.Errorf("heading not bold: %q", got)
	}
	if stripped(one("#hashtag")) != "#hashtag" {
		t.Errorf("no-space hash is not a heading")
	}
}

func TestStyle_Inline(t *testing.T) {
	cases := map[string]string{
		"a **bold** b":        "a bold b",
		"a *em* b":            "a em b",
		"a _em_ b":            "a em b",
		"a `code` b":          "a code b",
		"a ~~gone~~ b":        "a gone b",
		`\*not em\* \` + "`x": "*not em* `x",
		"**b *i* b**":         "b i b",
		"snake_case_name":     "snake_case_name",
		"2 * 3 * 4":           "2 * 3 * 4",
		"open **only":         "open **only",
		"``a`b``":             "a`b",
		"`**raw**`":           "**raw**",
	}
	for in, want := range cases {
		got := one(in)
		if stripped(got) != want {
			t.Errorf("%q -> %q, want text %q", in, stripped(got), want)
		}
		if want != in && !strings.Contains(got, "\x1b[") && !strings.Contains(in, `\`) {
			t.Errorf("%q has no styling: %q", in, got)
		}
	}
	if !strings.Contains(one("**b**"), "\x1b[1m") {
		t.Errorf("bold missing")
	}
	if !strings.Contains(one("~~s~~"), "\x1b[9m") {
		t.Errorf("strike missing")
	}
}

func TestStyle_FenceSuppressesInline(t *testing.T) {
	lines := []string{"```go", "x := **a** + `b` \\*", "```", "after **bold**"}
	out := runStyle(lines, 0, true)
	if stripped(out[0]) != "```go" {
		t.Errorf("fence line = %q", stripped(out[0]))
	}
	if stripped(out[1]) != "  x := **a** + `b` \\*" {
		t.Errorf("fenced line = %q", stripped(out[1]))
	}
	if stripped(out[3]) != "after bold" {
		t.Errorf("line after fence = %q", stripped(out[3]))
	}
	if !strings.Contains(out[1], "\x1b[2m") {
		t.Errorf("fenced line not dim: %q", out[1])
	}
}

func TestStyle_OpenFenceStaysDimUntilReset(t *testing.T) {
	var st lineStyle
	st.style("~~~", 0, true)
	if got := st.style("**x**", 0, true); stripped(got) != "  **x**" {
		t.Errorf("open fence line = %q", stripped(got))
	}
	st.reset()
	if got := st.style("**x**", 0, true); stripped(got) != "x" {
		t.Errorf("after reset = %q", stripped(got))
	}
}

func TestStyle_Table(t *testing.T) {
	out := runStyle([]string{"| h1 | h2 |", "|---|:--:|", "| **a** | b |", "text", "| h3 |"}, 0, true)
	if !strings.Contains(out[0], "\x1b[1m") || stripped(out[0]) != "| h1 | h2 |" {
		t.Errorf("header = %q", out[0])
	}
	if !strings.Contains(out[1], "\x1b[2m") || stripped(out[1]) != "|---|:--:|" {
		t.Errorf("separator = %q", out[1])
	}
	if stripped(out[2]) != "| a | b |" || strings.Contains(out[2], "\x1b[2m") {
		t.Errorf("row = %q", out[2])
	}
	if !strings.Contains(out[4], "\x1b[1m") {
		t.Errorf("second table header = %q", out[4])
	}
}

func TestStyle_ListsAndQuotes(t *testing.T) {
	cases := map[string]string{
		"- item **x**":  "- item x",
		"  * nested":    "  * nested",
		"12. numbered":  "12. numbered",
		"> quoted `c`":  "> quoted c",
		">> deep":       ">> deep",
		"---":           "---",
		"**bold** line": "bold line",
	}
	for in, want := range cases {
		if got := stripped(one(in)); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	if got := one("- item"); !strings.HasPrefix(got, "\x1b[") || !strings.HasSuffix(got, " item") {
		t.Errorf("only the marker is coloured: %q", got)
	}
}

func TestStyle_Wrap(t *testing.T) {
	got := one("alpha beta gamma delta")
	wrapped := runStyle([]string{"alpha beta gamma delta"}, 11, true)[0]
	if stripped(got) != "alpha beta gamma delta" {
		t.Fatalf("unwrapped = %q", got)
	}
	for _, l := range strings.Split(stripped(wrapped), "\n") {
		if len(l) > 11 {
			t.Errorf("line %q wider than 11", l)
		}
	}
	if strings.Join(strings.Fields(stripped(wrapped)), " ") != "alpha beta gamma delta" {
		t.Errorf("wrap lost text: %q", stripped(wrapped))
	}
}

func alnum(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// checkNoLoss is the styler's invariant: no panic, no lost letters or digits.
func checkNoLoss(t *testing.T, lines []string, width int) {
	t.Helper()
	var st lineStyle
	for _, l := range lines {
		got := st.style(l, width, true)
		if utf8.ValidString(l) && alnum(stripped(got)) != alnum(stripped(l)) {
			t.Fatalf("text lost: %q -> %q", l, stripped(got))
		}
	}
}

func TestStyle_HostileFixture(t *testing.T) {
	data, err := os.ReadFile("testdata/claude/fuzz.mdlive.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	checkNoLoss(t, lines, 0)
	checkNoLoss(t, lines, 20)
	for i, got := range runStyle(lines, 20, false) {
		if got != lines[i] {
			t.Fatalf("no-colour changed %q", lines[i])
		}
	}
}

func FuzzStyle(f *testing.F) {
	for _, s := range []string{"### h", "**a *b** c*", "`x", "``a`b``", "\\", "~~~", "| a |", "- **", "> `", "_a_b_", "\x1b[1m", "<!-- fold open: x -->"} {
		f.Add(s, s, 10)
	}
	f.Fuzz(func(t *testing.T, a, b string, width int) {
		if width < 0 || width > 200 {
			width = 0
		}
		lines := []string{a, b, a + b}
		var st lineStyle
		for _, l := range lines {
			if st.style(l, width, false) != l {
				t.Fatalf("no-colour changed %q", l)
			}
		}
		{
			checkNoLoss(t, lines, width)
		}
	})
}
