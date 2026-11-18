package prose

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Note is one finding. Line and Col are 1-based; Col counts characters.
type Note struct {
	Rule    string `json:"rule"`
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Match   string `json:"match"`
	Message string `json:"message"`
}

// Options tune one Check call.
type Options struct {
	// Step treats the whole text as one step: 20-word limit and the step-verb rule.
	Step bool
}

type finding struct {
	rule, match, msg string
	index            int
}

// Check returns every note for text, sorted by position. It never blocks.
func Check(text string, opts Options) []Note {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	t := strip(text)
	var fs []finding
	add := func(rule, match, msg string, index int) {
		fs = append(fs, finding{rule, match, msg, index})
	}
	checkTells(t, add)
	checkSemicolons(t, add)
	for _, pm := range paraRe.FindAllStringIndex(t, -1) {
		par := t[pm[0]:pm[1]]
		if strings.TrimSpace(par) == "" {
			continue
		}
		for _, sg := range segments(par, pm[0]) {
			for _, s := range sentences(sg, opts.Step) {
				checkSentence(s, add)
			}
		}
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].index < fs[j].index })
	notes := make([]Note, 0, len(fs))
	for _, f := range fs {
		line, col := position(text, f.index)
		notes = append(notes, Note{f.rule, line, col, f.match, f.msg})
	}
	return notes
}

// position converts a byte index into a 1-based line and character column.
func position(text string, index int) (int, int) {
	if index > len(text) {
		index = len(text)
	}
	before := text[:index]
	line := strings.Count(before, "\n") + 1
	col := utf8.RuneCountInString(before[strings.LastIndex(before, "\n")+1:]) + 1
	return line, col
}

func checkSemicolons(t string, add func(rule, match, msg string, index int)) {
	for i := 0; i < len(t); i++ {
		if t[i] == ';' && !entityEnd(t, i) {
			add(RuleSemicolon, ";", "Use two sentences instead of a semicolon.", i)
		}
	}
}

// entityEnd reports whether the ';' closes an HTML entity such as &amp;.
func entityEnd(t string, i int) bool {
	j := i - 1
	for j >= 0 && j > i-8 && (t[j] >= 'a' && t[j] <= 'z' || t[j] >= '0' && t[j] <= '9' || t[j] == '#') {
		j--
	}
	return j >= 0 && t[j] == '&' && j < i-1
}
