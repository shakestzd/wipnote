package render

import (
	"regexp"
	"strings"
)

// A run's clean.md read back as prose and folds (the mod's livedoc.ts and
// codex_view.py's LiveDoc): a fold is a tool result between the markers
// fold() writes, and a marker counts only outside a fence, so tool output
// and prose cannot make one.

// PartKind is prose or a fold.
type PartKind int

const (
	PartProse PartKind = iota
	PartFold
)

// Part is one prose block or one fold of clean.md.
type Part struct {
	Kind   PartKind
	Text   string // prose
	Title  string // fold
	Body   string // fold, its label dropped when the title repeats it
	Failed bool   // fold: opened expanded
}

var (
	foldOpenRe = regexp.MustCompile(`^<!-- fold (open|closed): (.*) -->$`)
	labelRe    = regexp.MustCompile(`^\*\*[A-Za-z0-9_.:-]+\*\*( .*)?$`)
)

const foldEnd = "<!-- /fold -->"

// docReader splits lines of clean.md into prose lines and folds.
type docReader struct {
	fence *fenceState
	fold  *openFold
}

type openFold struct {
	title  string
	failed bool
	lines  []string
	fence  *fenceState
}

// lineKind is what one line of clean.md was.
type lineKind int

const (
	lineProse    lineKind = iota // outside any fold
	lineFoldOpen                 // a fold's opening marker
	lineInFold                   // inside a fold
	lineFoldEnd                  // a fold's end marker: the fold is returned
)

// line takes one line and says what it was; a fold is returned at its end.
func (d *docReader) line(l string) (lineKind, Part) {
	if f := d.fold; f != nil {
		if f.fence == nil && l == foldEnd {
			d.fold = nil
			return lineFoldEnd, f.part()
		}
		f.lines = append(f.lines, l)
		f.fence = fenceStep(f.fence, l)
		return lineInFold, Part{}
	}
	if d.fence == nil {
		if m := foldOpenRe.FindStringSubmatch(l); m != nil {
			d.fold = &openFold{title: m[2], failed: m[1] == "open"}
			return lineFoldOpen, Part{}
		}
	}
	d.fence = fenceStep(d.fence, l)
	return lineProse, Part{}
}

// flush returns a fold the run never closed.
func (d *docReader) flush() (Part, bool) {
	f := d.fold
	d.fold = nil
	if f == nil {
		return Part{}, false
	}
	return f.part(), true
}

// part is the fold, its body without the label that repeats its title; a
// cut title ("...") keeps the label, the only place the whole command is.
func (f *openFold) part() Part {
	lines := f.lines
	if len(lines) > 0 && !strings.Contains(f.title, "...") && labelRe.MatchString(lines[0]) {
		lines = lines[1:]
	}
	body := strings.Trim(strings.Join(lines, "\n"), "\n")
	if body == "" {
		body = "*(empty)*"
	}
	return Part{Kind: PartFold, Title: f.title, Body: body, Failed: f.failed}
}

// ParseRun splits clean.md into parts. Only complete lines are read (a
// trailing partial line waits for its newline) and a fold whose end marker
// has not arrived is held back, unless final (the run ended).
func ParseRun(text string, final bool) []Part {
	if !final {
		text = text[:strings.LastIndexByte(text, '\n')+1]
	}
	var parts []Part
	var prose []string
	flush := func() {
		if trimSpace(strings.Join(prose, "")) != "" {
			parts = append(parts, Part{Kind: PartProse, Text: strings.TrimRight(strings.Join(prose, "\n"), "\n")})
		}
		prose = nil
	}
	var d docReader
	for _, l := range splitLines([]byte(text)) {
		switch kind, p := d.line(l); kind {
		case lineProse:
			prose = append(prose, l)
		case lineFoldOpen:
			flush()
		case lineFoldEnd:
			parts = append(parts, p)
		}
	}
	flush()
	if final {
		if p, ok := d.flush(); ok {
			parts = append(parts, p)
		}
	}
	return parts
}
