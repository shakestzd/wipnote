package render

import (
	"regexp"
	"strings"
	"unicode"
)

// wrapperRe finds a `sh -c`-style wrapper (bash, zsh, sh, dash or ksh, any
// path, flags such as -lc) around one shell word. As in md.jq (whose "s"
// flag is Oniguruma's single-line mode, not dot-all), the word cannot span a
// newline.
var wrapperRe = regexp.MustCompile(`^` + ws + `*(?:[^\t\n\v\f\r \x{85}\p{Z}]*/)?(?:bash|zsh|sh|dash|ksh)` +
	ws + `+-[a-z]*c[a-z]*` + ws + `+([^\t\n\v\f\r \x{85}\p{Z}].*?)` + ws + `*$`)

// metaChars stop an unquoted word: the outer shell would expand them.
const metaChars = "$`*?[]{}()<>|&;~!#"

// unwrapShell strips the wrapper and its quote layer from a command, reading
// the wrapped word the way the outer shell reads it. A word that would need
// the outer shell to expand something is not guessed at: the command is
// kept as written.
func unwrapShell(command string) string {
	m := wrapperRe.FindStringSubmatch(command)
	if m == nil {
		return command
	}
	if w, ok := shellword(m[1]); ok {
		return w
	}
	return command
}

// wordState reads one shell word a character at a time.
type wordState struct {
	mode byte // 'u' unquoted, 's' single quotes, 'd' double quotes
	w    strings.Builder
	esc  bool
	bad  bool
}

// shellword is the single word s spells, or false.
func shellword(s string) (string, bool) {
	st := wordState{mode: 'u'}
	for _, c := range s {
		switch st.mode {
		case 's':
			st.single(c)
		case 'd':
			st.double(c)
		default:
			st.unquoted(c)
		}
	}
	if st.mode != 'u' || st.esc || st.bad || st.w.Len() == 0 {
		return "", false
	}
	return st.w.String(), true
}

func (st *wordState) single(c rune) {
	if c == '\'' {
		st.mode = 'u'
		return
	}
	st.w.WriteRune(c)
}

// double: \\ \" \$ \` unescape; another backslash pair stays as written; an
// unescaped $ or backtick would expand.
func (st *wordState) double(c rune) {
	switch {
	case st.esc:
		st.esc = false
		switch {
		case strings.ContainsRune("\"\\$`", c):
			st.w.WriteRune(c)
		case c == '\n':
			st.bad = true
		default:
			st.w.WriteRune('\\')
			st.w.WriteRune(c)
		}
	case c == '\\':
		st.esc = true
	case c == '"':
		st.mode = 'u'
	case c == '$' || c == '`':
		st.bad = true
	default:
		st.w.WriteRune(c)
	}
}

// unquoted: any backslash pair unescapes; whitespace and metacharacters
// would split or expand the word.
func (st *wordState) unquoted(c rune) {
	switch {
	case st.esc:
		st.esc = false
		if c == '\n' {
			st.bad = true
			return
		}
		st.w.WriteRune(c)
	case c == '\\':
		st.esc = true
	case c == '\'':
		st.mode = 's'
	case c == '"':
		st.mode = 'd'
	case unicode.IsSpace(c) || strings.ContainsRune(metaChars, c):
		st.bad = true
	default:
		st.w.WriteRune(c)
	}
}
