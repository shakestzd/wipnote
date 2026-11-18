package prose

import (
	"regexp"
	"strings"
)

var (
	listLine = regexp.MustCompile(`^(\s*)(?:\d+[.)]|[-*•])\s+`)
	labelRe  = regexp.MustCompile(`^\*\*[^*\n]+\*\*:?\s*`)
	abbrevRe = regexp.MustCompile(`(?i)\b(e\.g|i\.e|etc|vs|approx)\.`)
	wordRe   = regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9'’_.\-/]*`)
	paraRe   = regexp.MustCompile(`[^\n]+(?:\n[^\S\n]*[^\s][^\n]*)*`)
)

type seg struct {
	text  string
	start int
	list  bool
}

// sentence is one sentence with its byte index in the stripped text.
type sentence struct {
	text  string
	index int
	list  bool
	first bool // first sentence of its list item or step
}

// segments splits a paragraph into prose runs and single list lines.
func segments(par string, base int) []seg {
	var segs []seg
	pos := 0
	for _, line := range strings.Split(par, "\n") {
		list := listLine.MatchString(line)
		if n := len(segs); !list && n > 0 && !segs[n-1].list {
			segs[n-1].text += " " + line
		} else {
			segs = append(segs, seg{line, base + pos, list})
		}
		pos += len(line) + 1
	}
	return segs
}

// sentences ports lint.ts: a bold label opening a list item is prose, not a step,
// and abbreviation dots do not end a sentence. forceStep treats prose as a step.
func sentences(s seg, forceStep bool) []sentence {
	skip := 0
	if m := listLine.FindString(s.text); s.list && m != "" {
		skip = len(m)
	}
	body := s.text[skip:]
	list := s.list || forceStep
	if s.list {
		if lb := labelRe.FindString(body); lb != "" {
			body = blank(lb) + body[len(lb):]
			list = forceStep
		}
	}
	// An item that opens with a code span is a name, not a step: the span is blank here
	// and the marker's greedy whitespace swallowed it.
	codeLead := s.list && strings.HasSuffix(listLine.FindString(s.text), "  ")
	masked := abbrevRe.ReplaceAllStringFunc(body, func(m string) string { return strings.ReplaceAll(m, ".", "_") })
	var out []sentence
	start := 0
	emit := func(end int) {
		raw := body[start:end]
		text := strings.TrimSpace(raw)
		if text != "" {
			lead := len(raw) - len(strings.TrimLeft(raw, " \t\r\n"))
			out = append(out, sentence{text, s.start + skip + start + lead, list, len(out) == 0 && !codeLead})
		}
		start = end
	}
	for i := 0; i < len(masked); i++ {
		if c := masked[i]; c != '.' && c != '!' && c != '?' {
			continue
		}
		j := i
		for j < len(masked) && (masked[j] == '.' || masked[j] == '!' || masked[j] == '?') {
			j++
		}
		if j == len(masked) || masked[j] == ' ' || masked[j] == '\t' || masked[j] == '\n' || masked[j] == '\r' {
			emit(j)
		}
		i = j - 1
	}
	emit(len(masked))
	return out
}

// wordCount counts words the way lint.ts does.
func wordCount(s string) int { return len(wordRe.FindAllString(s, -1)) }
