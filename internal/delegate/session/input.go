package session

import (
	"context"
	"strconv"
	"strings"
	"unicode"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// Input is the worker's input line as a styled read saw it. Read is false
// when the read failed or found no prompt line; that is never "empty".
type Input struct {
	Read bool
	Text string
	Dim  []bool // per rune of Text: drawn dim or grey
}

// Draft reports whether the line holds text a person may have typed: plain
// or mixed text, or a failed read. A line whose every visible character is
// dim or grey is the harness's next-prompt suggestion and reads as empty
// (orch inputstyle.is_suggestion).
func (in Input) Draft() bool {
	if !in.Read {
		return true
	}
	for i, r := range []rune(in.Text) {
		if !unicode.IsSpace(r) && (i >= len(in.Dim) || !in.Dim[i]) {
			return true
		}
	}
	return false
}

func (in Input) why() string {
	if !in.Read {
		return "the styled read of the input line failed, so it may hold a draft"
	}
	return "the line holds " + strconv.Quote(in.Text)
}

// StyledReader is a client that can read a pane with its SGR styling
// (herdr pane read --format ansi; UNVERIFIED until Phase B D8a/D8b pass).
type StyledReader interface {
	PaneReadStyled(ctx context.Context, pane herdr.PaneID, lines int) (string, error)
}

// ReadInput reads pane's input line with styling. A client without a styled
// read, or a failed read, gives Input{Read: false}.
func ReadInput(ctx context.Context, c herdr.Client, pane herdr.PaneID) Input {
	sr, ok := c.(StyledReader)
	if !ok {
		return Input{}
	}
	screen, err := sr.PaneReadStyled(ctx, pane, 12)
	if err != nil {
		return Input{}
	}
	return ParseInput(screen)
}

// promptMarks open the input line: Claude Code draws ">" or "❯", Codex "›".
var promptMarks = []string{">", "❯", "›"}

// ParseInput finds the last prompt line of an ANSI screen and returns the
// text after the prompt mark with each rune's dim flag. A box border ("│")
// around the line is dropped. Continuation lines below the prompt line are
// part of the input until a blank line or a box rule ends it, so a draft
// whose first line is empty still reads as a draft.
func ParseInput(screen string) Input {
	lines := strings.Split(strings.TrimRight(screen, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		text, dim := styledRunes(lines[i])
		start := skipFrame(text, 0)
		for _, m := range promptMarks {
			mr := []rune(m)
			if hasRunes(text, start, mr) {
				from := start + len(mr)
				if from < len(text) && text[from] == ' ' {
					from++
				}
				in := Input{Read: true}
				in.add(text, dim, from)
				in.addContinuation(lines[i+1:])
				return in
			}
		}
	}
	return Input{}
}

// add appends text[from:] without its trailing spaces and right border.
func (in *Input) add(text []rune, dim []bool, from int) {
	end := len(text)
	for end > from && (unicode.IsSpace(text[end-1]) || text[end-1] == '│' || text[end-1] == '┃') {
		end--
	}
	if from < end {
		in.Text += string(text[from:end])
		in.Dim = append(in.Dim, dim[from:end]...)
	}
}

// addContinuation appends the input's continuation lines: those after the
// prompt line up to the first blank line or box rule.
func (in *Input) addContinuation(lines []string) {
	for _, line := range lines {
		text, dim := styledRunes(line)
		from := skipFrame(text, 0)
		if from >= len(text) || strings.ContainsRune("─━═╰╭└┌", text[from]) {
			return
		}
		in.Text += "\n"
		in.Dim = append(in.Dim, true)
		in.add(text, dim, from)
	}
}

func skipFrame(text []rune, i int) int {
	for i < len(text) && (unicode.IsSpace(text[i]) || text[i] == '│' || text[i] == '┃') {
		i++
	}
	return i
}

func hasRunes(text []rune, at int, want []rune) bool {
	if at+len(want) > len(text) {
		return false
	}
	for j, r := range want {
		if text[at+j] != r {
			return false
		}
	}
	return true
}

// styledRunes strips SGR sequences from line and reports, per visible rune,
// whether it was drawn dim (SGR 2) or in a grey foreground.
func styledRunes(line string) ([]rune, []bool) {
	var text []rune
	var dims []bool
	var faint, greyFG bool
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		if rs[i] == 0x1b && i+1 < len(rs) && rs[i+1] == '[' {
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				faint, greyFG = applySGR(string(rs[i+2:j]), faint, greyFG)
			}
			i = j
			continue
		}
		text = append(text, rs[i])
		dims = append(dims, faint || greyFG)
	}
	return text, dims
}

// applySGR folds one SGR parameter list into the faint and grey flags.
func applySGR(params string, faint, grey bool) (bool, bool) {
	ps := strings.Split(params, ";")
	for k := 0; k < len(ps); k++ {
		switch p := ps[k]; p {
		case "", "0":
			faint, grey = false, false
		case "2":
			faint = true
		case "22":
			faint = false
		case "90":
			grey = true
		case "39":
			grey = false
		case "38":
			var used int
			grey, used = extendedGrey(ps[k+1:])
			k += used
		default:
			if n, err := strconv.Atoi(p); err == nil && n >= 30 && n <= 97 && n != 90 && (n <= 37 || n >= 91) {
				grey = false
			}
		}
	}
	return faint, grey
}

// extendedGrey reads the rest of a 38 sequence: 5;N (8 or 232-250 are grey)
// or 2;R;G;B (a mid grey). It returns whether it is grey and how many
// parameters it used.
func extendedGrey(rest []string) (bool, int) {
	num := func(i int) int {
		if i >= len(rest) {
			return -1
		}
		n, err := strconv.Atoi(rest[i])
		if err != nil {
			return -1
		}
		return n
	}
	switch num(0) {
	case 5:
		n := num(1)
		return n == 8 || (n >= 232 && n <= 250), 2
	case 2:
		r, g, b := num(1), num(2), num(3)
		near := func(a, b int) bool { return a-b < 16 && b-a < 16 }
		return near(r, g) && near(g, b) && r >= 64 && r <= 176, 4
	}
	return false, 0
}

var _ StyledReader = (*herdr.CLI)(nil)
