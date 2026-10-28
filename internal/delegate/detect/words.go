package detect

import "strings"

// word is one shell word: its text with quotes and escapes removed, and the
// offset in the command where it starts.
type word struct {
	text  string
	start int
}

// lexer splits one standalone command into shell words. It knows quotes,
// backslash escapes and continuations, comments and output redirections
// (dropped with their target). Expansions ($VAR, globs) stay as written: the
// words only feed delegate run's argv classifier.
type lexer struct {
	s     string
	i     int
	cur   strings.Builder
	start int // -1: between words
	drop  bool
	out   []word
}

// splitWords is the command's words. ok is false when the command must run as
// typed: an unterminated quote, or input on stdin (<, <<, <<<), which
// delegate run does not forward.
func splitWords(s string) ([]word, bool) {
	l := &lexer{s: s, start: -1}
	for l.i < len(s) {
		if !l.step() {
			return nil, false
		}
	}
	l.flush()
	return l.out, true
}

func (l *lexer) step() bool {
	switch c := l.s[l.i]; {
	case c == '\\':
		l.escape()
	case c == '\'':
		return l.single()
	case c == '"':
		return l.double()
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		l.flush()
		l.i++
	case c == '<':
		return false
	case c == '>' || c == '&' && strings.HasPrefix(l.s[l.i:], "&>"):
		l.redirect()
	case c == '#' && l.start < 0:
		l.flush()
		l.i = len(l.s)
	default:
		l.add(l.i, c)
		l.i++
	}
	return true
}

func (l *lexer) add(at int, c byte) {
	if l.start < 0 {
		l.start = at
	}
	l.cur.WriteByte(c)
}

func (l *lexer) flush() {
	if l.start < 0 {
		return
	}
	if l.drop {
		l.drop = false
	} else {
		l.out = append(l.out, word{l.cur.String(), l.start})
	}
	l.cur.Reset()
	l.start = -1
}

// escape: a backslash-newline joins lines; any other backslash quotes the
// next byte.
func (l *lexer) escape() {
	if l.i+1 >= len(l.s) {
		l.add(l.i, '\\')
		l.i++
		return
	}
	if l.s[l.i+1] != '\n' {
		l.add(l.i, l.s[l.i+1])
	}
	l.i += 2
}

func (l *lexer) single() bool {
	end := strings.IndexByte(l.s[l.i+1:], '\'')
	if end < 0 {
		return false
	}
	if l.start < 0 {
		l.start = l.i
	}
	l.cur.WriteString(l.s[l.i+1 : l.i+1+end])
	l.i += end + 2
	return true
}

func (l *lexer) double() bool {
	if l.start < 0 {
		l.start = l.i
	}
	for j := l.i + 1; j < len(l.s); j++ {
		switch c := l.s[j]; {
		case c == '"':
			l.i = j + 1
			return true
		case c == '\\' && j+1 < len(l.s) && strings.IndexByte("$`\"\\\n", l.s[j+1]) >= 0:
			if l.s[j+1] != '\n' {
				l.cur.WriteByte(l.s[j+1])
			}
			j++
		default:
			l.cur.WriteByte(c)
		}
	}
	return false
}

// redirect drops an output redirection: an fd prefix (2>), the operator
// (>, >>, >&, >|, &>, &>>) and its target, attached or the next word.
func (l *lexer) redirect() {
	if l.start >= 0 && isDigits(l.cur.String()) {
		l.cur.Reset()
		l.start = -1
	}
	l.flush()
	l.i++ // > or &
	for l.i < len(l.s) && strings.IndexByte(">&|", l.s[l.i]) >= 0 {
		l.i++
	}
	l.drop = true
}

func isDigits(s string) bool {
	return s != "" && strings.Trim(s, "0123456789") == ""
}
