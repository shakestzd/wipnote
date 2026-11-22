package render

import (
	"io"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// The live pane styles each complete line once, as it is printed. A line is
// never redrawn, so a marker that spans lines is left as written.

// maxInlineLine is the longest line given inline styling; the scan for a
// closing marker is quadratic, so a longer line is printed as it is.
const maxInlineLine = 2000

// maxInlineDepth bounds nesting of inline styles.
const maxInlineDepth = 6

// theme holds the styles. Its renderer has a fixed colour profile so output
// does not depend on the host terminal; the pane only styles on a TTY.
type theme struct {
	plain, bold, dim, code, head, marker, quote lipgloss.Style
	ok, fail                                    lipgloss.Style
}

var styles = newTheme()

func newTheme() theme {
	r := lipgloss.NewRenderer(io.Discard)
	r.SetColorProfile(termenv.ANSI256)
	s := r.NewStyle()
	return theme{
		plain:  s,
		bold:   s.Bold(true),
		dim:    s.Faint(true),
		code:   s.Foreground(lipgloss.Color("179")),
		head:   s.Bold(true).Foreground(lipgloss.Color("39")),
		marker: s.Foreground(lipgloss.Color("33")),
		quote:  s.Foreground(lipgloss.Color("244")),
		ok:     s.Foreground(lipgloss.Color("42")),
		fail:   s.Foreground(lipgloss.Color("203")),
	}
}

// lineStyle is what the styler remembers between lines.
type lineStyle struct {
	fence *fenceState // an open code fence
	table bool        // the previous line was a table row
}

// reset forgets the open fence and table, at a rewrite or the run's end.
func (s *lineStyle) reset() { *s = lineStyle{} }

var (
	headRe      = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+`)
	styleListRe = regexp.MustCompile(`^(\s*)([-*+]|\d{1,9}[.)])(\s+)(.*)$`)
	quoteRe     = regexp.MustCompile(`^\s{0,3}>+ ?`)
	sepRe       = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
)

// style returns line as it is printed: styled, and wrapped to width when
// width is above zero. Without colour it returns line unchanged.
func (s *lineStyle) style(line string, width int, colour bool) string {
	if !colour {
		return line
	}
	out := s.styleLine(line)
	if width > 0 {
		out = ansi.Wrap(out, width, "")
	}
	return out
}

func (s *lineStyle) styleLine(l string) string {
	if out, ok := s.fenced(l); ok {
		s.table = false
		return out
	}
	trimmed := strings.TrimSpace(l)
	if strings.HasPrefix(trimmed, "|") {
		return s.tableRow(l)
	}
	s.table = false
	if m := headRe.FindString(l); m != "" {
		return styleInline(l[len(m):], styles.head, 0)
	}
	if m := quoteRe.FindString(l); m != "" {
		return styles.quote.Render(m) + styleInline(l[len(m):], styles.plain, 0)
	}
	if m := styleListRe.FindStringSubmatch(l); m != nil {
		return m[1] + styles.marker.Render(m[2]) + m[3] + styleInline(m[4], styles.plain, 0)
	}
	return styleInline(l, styles.plain, 0)
}

// fenced handles a fence line and the lines inside a fence.
func (s *lineStyle) fenced(l string) (string, bool) {
	if s.fence == nil {
		if s.fence = fenceOpen(l); s.fence == nil {
			return "", false
		}
		return styles.dim.Render(l), true
	}
	s.fence = fenceStep(s.fence, l)
	if s.fence == nil { // the closing fence
		return styles.dim.Render(l), true
	}
	return styles.dim.Render("  " + l), true
}

// tableRow: the first row of a table is its header, a |---| row is dim.
func (s *lineStyle) tableRow(l string) string {
	header := !s.table
	s.table = true
	switch {
	case sepRe.MatchString(l) && strings.Contains(l, "-"):
		return styles.dim.Render(l)
	case header:
		return styleInline(l, styles.bold, 0)
	}
	return styleInline(l, styles.plain, 0)
}

func isPunct(c byte) bool {
	return c > ' ' && c < 0x7f && !(c >= '0' && c <= '9') && !(c|0x20 >= 'a' && c|0x20 <= 'z')
}

func isWord(c byte) bool {
	return c >= 0x80 || (c >= '0' && c <= '9') || (c|0x20 >= 'a' && c|0x20 <= 'z')
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

// styleInline styles the emphasis, code and strike spans of one line; text with
// no closing marker, and text after a backslash, is kept as written.
func styleInline(s string, base lipgloss.Style, depth int) string {
	if len(s) > maxInlineLine || depth > maxInlineDepth {
		return base.Render(s)
	}
	var out, lit strings.Builder
	emit := func(styled string) {
		if lit.Len() > 0 {
			out.WriteString(base.Render(lit.String()))
			lit.Reset()
		}
		out.WriteString(styled)
	}
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '\\' && i+1 < len(s) && isPunct(s[i+1]):
			lit.WriteByte(s[i+1])
			i += 2
		case c == '`':
			n := runLen(s, i)
			if j := codeEnd(s, i+n, n); j >= 0 {
				emit(styles.code.Render(codeText(s[i+n : j])))
				i = j + n
			} else {
				lit.WriteString(s[i : i+n])
				i += n
			}
		case c == '*' || c == '_' || c == '~':
			m := min(runLen(s, i), 2)
			if end, st := span(s, i, m, base); end >= 0 {
				emit(styleInline(s[i+m:end], st, depth+1))
				i = end + m
			} else {
				lit.WriteString(s[i : i+m])
				i += m
			}
		default:
			lit.WriteByte(c)
			i++
		}
	}
	emit("")
	return out.String()
}

// runLen is the length of the run of s[i] starting at i.
func runLen(s string, i int) int {
	n := 1
	for i+n < len(s) && s[i+n] == s[i] {
		n++
	}
	return n
}

// codeEnd finds a closing run of exactly n backticks from start, or -1.
func codeEnd(s string, start, n int) int {
	for j := start; j < len(s); {
		if s[j] != '`' {
			j++
			continue
		}
		if k := runLen(s, j); k == n {
			return j
		} else {
			j += k
		}
	}
	return -1
}

// codeText trims the single space that pads a span holding a backtick.
func codeText(t string) string {
	if len(t) > 2 && t[0] == ' ' && t[len(t)-1] == ' ' && strings.Trim(t, " ") != "" {
		return t[1 : len(t)-1]
	}
	return t
}

// span finds the closing marker for the m-long marker at i and the style of
// what it holds; end is -1 when it is not a span. A single "~" is no span.
func span(s string, i, m int, base lipgloss.Style) (int, lipgloss.Style) {
	c := s[i]
	st := base.Italic(true)
	switch {
	case c == '~' && m < 2:
		return -1, base
	case c == '~':
		st = base.Strikethrough(true)
	case m == 2:
		st = base.Bold(true)
	}
	if i+m >= len(s) || isSpace(s[i+m]) || (c == '_' && i > 0 && isWord(s[i-1])) {
		return -1, base
	}
	for k := i + m; k < len(s); {
		switch {
		case s[k] == '\\':
			k += 2
		case s[k] == '`':
			n := runLen(s, k)
			if j := codeEnd(s, k+n, n); j >= 0 {
				k = j + n
			} else {
				k += n
			}
		case s[k] == c && runLen(s, k) == m && k > i+m && !isSpace(s[k-1]) &&
			(c != '_' || k+m >= len(s) || !isWord(s[k+m])):
			return k, st
		case s[k] == c:
			k += runLen(s, k)
		default:
			k++
		}
	}
	return -1, base
}
