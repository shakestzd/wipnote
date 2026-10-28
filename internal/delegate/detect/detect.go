// Package detect recognises a bare delegation in a Bash command: codex exec
// (or e) and review, ccx task, and claude -p or --print, bare or behind
// dev exec (plan-28b4a7f6 slice 9). It is the Go port of the recogniser in the
// retired herdr pane mod (its hooks/tools.ts), whose tables its tests
// repeat, and it backs the PreToolUse hook that redirects a bare standalone
// delegation to `wipnote delegate run` (Redirect).
//
// The mod's patterns use lookarounds and backreferences, which RE2 lacks: a
// trailing (?=\s|$) becomes a consumed (?:\s|$), the heredoc rule is a small
// scanner (heredoc.go), and claude's "not a subcommand" lookahead is a second
// match at the end of each claude found.
package detect

import (
	"regexp"
	"strings"
)

// Tool is the delegated tool a command invokes, or None.
type Tool string

const (
	None   Tool = ""
	Codex  Tool = "codex"
	Ccx    Tool = "ccx"
	Claude Tool = "claude"
)

const (
	// cmdPos is a command position: the start, or after ; & | ( or a newline,
	// behind optional VAR=value words.
	cmdPos = `(?:^|[;&|(\n])\s*(?:[A-Za-z_][A-Za-z0-9_]*=\S*\s+)*`
	ws     = `(?:[ \t]|\\\n)+`
	// devExec is the optional `dev exec [-d <dir>]` helper, behind which any
	// path to claude is the container's.
	devExec    = `(?:dev` + ws + `exec` + ws + `(?:-d(?:` + ws + `|=)?\S+` + ws + `)?(?:\S*/)?)?`
	claudeSubs = `mcp|plugins?|auth|agents|attach|auto-mode|daemon|doctor|gateway|import|install|logs|purge|remote-control|respawn|rm|self-hosted-runner|setup-token|stop|kill|ultrareview|update`
)

var (
	codexRe       = regexp.MustCompile(cmdPos + `codex\s+(?:exec|e|review)(?:\s|$)`)
	ccxRe         = regexp.MustCompile(cmdPos + `(?:\S*/)?ccx\s+task(?:\s|$)`)
	claudeAtRe    = regexp.MustCompile(cmdPos + devExec + `claude`)
	claudeSubRe   = regexp.MustCompile(`^` + ws + `(?:` + claudeSubs + `)(?:\s|$)`)
	claudePrintRe = regexp.MustCompile(`^(?:` + ws + `[^\s;&|()]+)*?` + ws + `(?:-p|--print)(?:\s|$)`)

	infoOnlyRe  = regexp.MustCompile(`\s(?:--help|-h|--version|-V)(?:\s|$)`)
	ownFormatRe = regexp.MustCompile(`\s--output-format(?:=|\s|$)`)
	quotedRe    = regexp.MustCompile(`'[^']*'|"(?:\\.|[^"\\])*"`)
	redirectRe  = regexp.MustCompile(`\s*(?:&>>?|\d*>>?|\d*<)&?[^\s;&|]*`)
	chainRe     = regexp.MustCompile("[;&|\n`(){}]|\\$\\(")
)

// Blank empties quoted strings and drops heredoc bodies: they hold prompts and
// text, not commands. An unterminated quote or heredoc is left as it is.
func Blank(command string) string {
	return quotedRe.ReplaceAllString(removeHeredocs(command), `""`)
}

// Find names the tool command invokes at a command position, or None. An
// info flag (--help, -h, --version, -V) means no run: anywhere in the command
// for codex and ccx, among claude's own words for claude, so
// `claude -p "x" | sort -h` is still a run. A claude run that names its own
// --output-format is left as asked.
func Find(command string) Tool {
	text := Blank(command)
	for _, t := range []struct {
		tool Tool
		re   *regexp.Regexp
	}{{Codex, codexRe}, {Ccx, ccxRe}} {
		if t.re.MatchString(text) {
			if infoOnlyRe.MatchString(text) {
				return None
			}
			return t.tool
		}
	}
	at, ok := claudeIndex(text)
	if !ok {
		return None
	}
	own := ownWords(text[at:])
	if infoOnlyRe.MatchString(own) || ownFormatRe.MatchString(own) {
		return None
	}
	return Claude
}

// claudeIndex is where the first claude print run in text starts: a claude at
// a command position whose first word is no subcommand and whose own words,
// on its line or across backslash-newlines, hold -p or --print.
func claudeIndex(text string) (int, bool) {
	for _, m := range claudeAtRe.FindAllStringIndex(text, -1) {
		rest := text[m[1]:]
		if !claudeSubRe.MatchString(rest) && claudePrintRe.MatchString(rest) {
			return m[0], true
		}
	}
	return 0, false
}

// ownWords are the invocation's words up to the next separator.
func ownWords(text string) string {
	text = strings.ReplaceAll(text, "\\\n", " ")
	if text != "" && strings.ContainsRune(";&|(\n", rune(text[0])) {
		text = text[1:]
	}
	if i := strings.IndexAny(text, ";&|\n"); i >= 0 {
		return text[:i]
	}
	return text
}

// Standalone reports whether command is exactly one delegation: with quoted
// text and heredoc bodies blanked and every redirection removed, it holds no
// ; & | newline, backtick, $( or ( ) { }. Anything else is chained, which v1
// leaves alone (q-chained). A backslash-newline joins lines of one command, so
// it is no separator (the mod has no case for it).
func Standalone(command string) bool {
	if Find(command) == None {
		return false
	}
	text := strings.ReplaceAll(Blank(command), "\\\n", " ")
	text = strings.TrimSpace(redirectRe.ReplaceAllString(text, " "))
	return !chainRe.MatchString(text)
}
