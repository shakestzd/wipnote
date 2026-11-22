package prose

import (
	"regexp"
	"strings"
)

var (
	inlineCode = regexp.MustCompile("`[^`\n]*`")
	urlRe      = regexp.MustCompile(`https?://\S+`)
	// A path starts with ./ ../ ~/ or /, or has a slash and a dotted last part.
	// Group 1 is the text before the path; only group 2 is blanked, so "and/or" stays.
	pathRe    = regexp.MustCompile(`(^|[\s(\["'])((?:(?:\.{1,2}|~)?/[\w.\-@]+(?:/[\w.\-@]+)*|[\w.\-@]+(?:/[\w.\-@]+)*/[\w\-@]+\.\w+))`)
	fenceOpen = regexp.MustCompile("^\\s*(```+|~~~+)")
	tableRow  = regexp.MustCompile(`^\s*\|`)
)

// blank replaces every byte but newlines with a space, so offsets stay valid.
func blank(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c != '\n' {
			b[i] = ' '
		}
	}
	return string(b)
}

// strip blanks fenced code, table rows, inline code, URLs and file paths.
// Same length as the input, so an index in the result is an index in the text.
func strip(text string) string {
	fence := ""
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		m := fenceOpen.FindStringSubmatch(line)
		switch {
		case fence != "":
			if m != nil && m[1][0] == fence[0] && len(m[1]) >= len(fence) {
				fence = ""
			}
			lines[i] = blank(line)
		case m != nil:
			fence = m[1]
			lines[i] = blank(line)
		case tableRow.MatchString(line):
			lines[i] = blank(line)
		}
	}
	out := strings.Join(lines, "\n")
	for _, re := range []*regexp.Regexp{inlineCode, urlRe} {
		out = re.ReplaceAllStringFunc(out, blank)
	}
	b := []byte(out)
	for _, m := range pathRe.FindAllStringSubmatchIndex(out, -1) {
		copy(b[m[4]:m[5]], blank(out[m[4]:m[5]]))
	}
	return string(b)
}
