package render

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// The one place that puts untrusted text into Markdown safely (the mod's
// md.jq). Every function returns one complete block, opened and closed, so a
// stream cut between two events never leaves a dangling fence.

var (
	ansiRe     = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
	escRe      = regexp.MustCompile("[\\\\`*_\\[\\]<>#|~&]")
	toolNameRe = regexp.MustCompile(`[^A-Za-z0-9_.:-]`)
	foldmarkRe = regexp.MustCompile(`^ {0,3}<!-- ?/?fold`)
	arrowRe    = regexp.MustCompile(`--+>`)
	mcpNoteRe  = regexp.MustCompile(`^! an MCP server was unreachable`)
	stdinRe    = regexp.MustCompile(`^` + ws + `*Reading additional input from stdin`)
)

// ws is jq's (Oniguruma's) \s: Unicode White_Space, which Go's \s is not.
const ws = `[\t\n\v\f\r \x{85}\p{Z}]`

const (
	xcrunNote = "*macOS xcrun cache errors hidden (sandbox), see raw.log*"
	mcpNote   = "*MCP server unreachable, see raw.log*"
)

// xcrunNoise: the macOS developer-tools errors git prints inside Codex's
// read-only sandbox, each anchored to the whole line.
var xcrunNoise = []*regexp.Regexp{
	regexp.MustCompile(`^[A-Za-z0-9_.-]+: warning: confstr\(\) failed with code [0-9]+: couldn't get path of DARWIN_USER_[A-Z_]+; using /tmp instead$`),
	regexp.MustCompile(`^[A-Za-z0-9_.-]+: error: couldn't create cache file '/tmp/xcrun_db-[^']*' \(errno=[^)]*\)$`),
	regexp.MustCompile(`^([0-9-]+ [0-9:.]+ )?xcodebuild\[[0-9]+:[0-9]+\] +DVTFilePathFSEvents: Failed to start fs event stream\.$`),
	regexp.MustCompile(`^([0-9-]+ [0-9:.]+ xcodebuild\[[0-9]+:[0-9]+\] )?\[MT\] DVTDeveloperPaths: Failed to get length of DARWIN_USER_CACHE_DIR from confstr\(3\), error = `),
}

func isXcrunNoise(line string) bool {
	for _, re := range xcrunNoise {
		if re.MatchString(line) {
			return true
		}
	}
	return false
}

// plain drops ANSI escapes and carriage returns.
func plain(s string) string {
	return strings.ReplaceAll(ansiRe.ReplaceAllString(s, ""), "\r", "")
}

// ticks is the longest backtick run in s.
func ticks(s string) int {
	best, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			best = max(best, run)
		} else {
			run = 0
		}
	}
	return best
}

// fenced puts s in a code fence longer than any backtick run inside it.
func fenced(s, info string) string {
	f := strings.Repeat("`", max(3, ticks(s)+1))
	return f + info + "\n" + s + "\n" + f
}

// code is an inline code span, its delimiter sized the same way.
func code(s string) string {
	t := collapse(s)
	d := strings.Repeat("`", ticks(t)+1)
	if strings.HasPrefix(t, "`") || strings.HasSuffix(t, "`") {
		return d + " " + t + " " + d
	}
	return d + t + d
}

// esc backslash-escapes Markdown punctuation in text that stays inline.
func esc(s string) string { return escRe.ReplaceAllString(s, `\$0`) }

// toolline is a call's bold label and its argument as inline code.
func toolline(name, arg string) string {
	out := "**" + toolNameRe.ReplaceAllString(name, "") + "**"
	if arg != "" {
		out += " " + code(arg)
	}
	return out
}

func errquote(msg string) string {
	if msg == "" {
		return "> **error**"
	}
	return "> **error** " + esc(cut(collapse(msg), 300))
}

// result is a tool result: an optional error quote, then the body in a fence
// (xcrun noise moved out of it into one note after it), cut to max chars.
func result(text string, maxChars int, isErr bool, msg string) string {
	all := strings.Split(strings.TrimRight(plain(text), "\n"), "\n")
	kept := make([]string, 0, len(all))
	for _, l := range all {
		if !isXcrunNoise(l) {
			kept = append(kept, l)
		}
	}
	hid := len(all) > len(kept)
	body := strings.TrimRight(strings.Join(kept, "\n"), "\n")
	if hid && body == "" {
		body = "(no output)"
	}
	if n := runeLen(body); n > maxChars {
		body = string([]rune(body)[:maxChars]) + fmt.Sprintf("\n... [truncated, %d more characters]", n-maxChars)
	}
	out := ""
	if isErr {
		out = errquote(msg) + "\n\n"
	}
	out += fenced(body, "")
	if hid {
		out += "\n\n" + xcrunNote
	}
	return out
}

// resultsize is how many lines a result shows, for a fold's title.
func resultsize(text string) string {
	n := 0
	for _, l := range strings.Split(strings.TrimRight(plain(text), "\n"), "\n") {
		if l != "" && !isXcrunNoise(l) {
			n++
		}
	}
	switch n {
	case 0:
		return "no output"
	case 1:
		return "1 line"
	}
	return fmt.Sprintf("%d lines", n)
}

// fenceState is an open CommonMark fence: its character and length.
type fenceState struct {
	c byte
	n int
}

// fenceOpen reads an opening fence: 3+ backticks or tildes indented under
// four spaces; a backtick fence's info string has no backtick.
func fenceOpen(line string) *fenceState {
	rest := strings.TrimPrefix(line, strings.Repeat(" ", min(indent(line), 3)))
	if rest == "" || (rest[0] != '`' && rest[0] != '~') {
		return nil
	}
	c := rest[0]
	n := len(rest) - len(strings.TrimLeft(rest, string(c)))
	if n < 3 || (c == '`' && strings.Contains(rest[n:], "`")) {
		return nil
	}
	return &fenceState{c: c, n: n}
}

// fenceStep is the fence state after line: an opening fence opens, a line of
// the same character at least as long (then only whitespace) closes.
func fenceStep(state *fenceState, line string) *fenceState {
	if state == nil {
		return fenceOpen(line)
	}
	if indent(line) > 3 {
		return state
	}
	rest := line[indent(line):]
	n := len(rest) - len(strings.TrimLeft(rest, string(state.c)))
	if n >= state.n && strings.TrimFunc(rest[n:], unicode.IsSpace) == "" {
		return nil
	}
	return state
}

func indent(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

func isFoldmark(line string) bool { return foldmarkRe.MatchString(line) }

// prose is assistant text as written, with any fence it leaves open closed
// again and any fold-marker-like line outside a fence escaped.
func prose(t string) string {
	lines := strings.Split(t, "\n")
	var state *fenceState
	for i, l := range lines {
		if state == nil && isFoldmark(l) {
			lines[i] = strings.Replace(l, "<!--", `\<!--`, 1)
		}
		state = fenceStep(state, l)
	}
	out := strings.Join(lines, "\n")
	if state != nil {
		out += "\n" + strings.Repeat(string(state.c), state.n)
	}
	return out
}

func foldtitle(s string) string {
	return arrowRe.ReplaceAllString(cut(collapse(plain(s)), 300), "->")
}

// fold wraps a tool result in the markers the viewers show as one
// collapsible section; a failed result opens expanded.
func fold(title string, open bool, body string) string {
	state := "closed"
	if open {
		state = "open"
	}
	return "<!-- fold " + state + ": " + foldtitle(title) + " -->\n" + body + "\n<!-- /fold -->"
}

// mdline is a line that is not an event, as Markdown: the stdin note
// dropped, the collapsed MCP warning an italic note, anything else in a
// text fence. ok is false when the line shows nothing.
func mdline(line string) (string, bool) {
	switch {
	case mcpNoteRe.MatchString(line):
		return mcpNote, true
	case stdinRe.MatchString(line), line == "":
		return "", false
	}
	return fenced(plain(line), "text"), true
}
