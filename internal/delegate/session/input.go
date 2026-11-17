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
	if from >= end {
		return
	}
	in.Text += string(text[from:end])
	if isNotice(text[from:end]) {
		for range text[from:end] {
			in.Dim = append(in.Dim, true)
		}
		return
	}
	in.Dim = append(in.Dim, dim[from:end]...)
}

// isNotice reports a line segment that wipnote itself printed to the
// worker's terminal, such as "wipnote: warning: shared collector moved…".
// Every wipnote stderr notice starts "wipnote: ", so the segment is not a
// person's draft and must not keep delegate stop from closing a finished
// pane (bug-440acb23).
func isNotice(seg []rune) bool {
	return strings.HasPrefix(strings.TrimSpace(string(seg)), "wipnote: ")
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
		if rs[i] == 0x1b || (rs[i] >= 0x80 && rs[i] <= 0x9f) {
			end, params, sgr := skipEscape(rs, i)
			if sgr {
				faint, greyFG = applySGR(params, faint, greyFG)
			}
			i = end
			continue
		}
		if rs[i] < 0x20 || rs[i] == 0x7f {
			continue // other control characters draw nothing
		}
		text = append(text, rs[i])
		dims = append(dims, faint || greyFG)
	}
	return text, dims
}

// skipEscape returns the index of the last rune of the escape sequence that
// starts at rs[i] (ESC or a C1 control), and, for an SGR sequence, its
// parameters. Besides CSI it skips the control strings a pane writes onto
// its own lines (OSC titles and Herdr's OSC 7501 state, DCS, SOS, PM, APC)
// through BEL or ST, and two-rune escapes such as a charset select, so none
// of their bytes read as typed text (bug-440acb23, #344).
func skipEscape(rs []rune, i int) (end int, params string, sgr bool) {
	intro, j := rs[i], i+1
	if intro == 0x1b {
		if j >= len(rs) {
			return i, "", false
		}
		switch rs[j] {
		case '[':
			intro = 0x9b
		case ']':
			intro = 0x9d
		case 'P', 'X', '^', '_':
			intro = 0x90 // control strings end the same way
		default:
			for j < len(rs) && rs[j] >= 0x20 && rs[j] <= 0x2f {
				j++ // intermediate bytes, then one final byte
			}
			return min(j, len(rs)-1), "", false
		}
		j++
	}
	switch intro {
	case 0x9b: // CSI: parameters and intermediates, then a final byte
		k := j
		for k < len(rs) && (rs[k] < 0x40 || rs[k] > 0x7e) {
			k++
		}
		if k < len(rs) && rs[k] == 'm' {
			return k, string(rs[j:k]), true
		}
		return min(k, len(rs)-1), "", false
	case 0x9d, 0x90, 0x98, 0x9e, 0x9f: // OSC, DCS, SOS, PM, APC
		for k := j; k < len(rs); k++ {
			switch {
			case rs[k] == 0x07 || rs[k] == 0x9c:
				return k, "", false
			case rs[k] == 0x1b && k+1 < len(rs) && rs[k+1] == '\\':
				return k + 1, "", false
			}
		}
		return len(rs) - 1, "", false
	}
	return i, "", false // any other C1 control is one rune
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

var (
	_ StyledReader = (*herdr.CLI)(nil)
	_ StyledReader = (*herdr.Core)(nil)
)
