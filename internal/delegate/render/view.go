package render

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/term"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ViewOptions tunes View.
type ViewOptions struct {
	Poll time.Duration // how often the run folder is read; 400ms when zero
	// Asks, when set, lists an interactive run's pending asks (hook asks
	// and outbox q-files, D7); each new set is printed once.
	Asks func() []string
	// Color forces styling on or off; nil styles only when the writer is a
	// terminal and NO_COLOR is unset.
	Color *bool
	// Width reads the terminal width for each line; nil asks the terminal,
	// and zero or less means no wrapping.
	Width func() int
}

// tailAnswer is how much of clean.log stands in for a missing answer.
const tailAnswer = 6000

// View follows one run in a plain terminal (wipnote delegate view, which
// replaces the mod's Textual viewer): clean.md as it grows, each tool result
// one line (a failed one with its output), clean.log while there is no
// clean.md. Only complete lines are printed until the run ends. When the
// exit file appears it prints the rest, the status and the answer, and
// returns.
func View(ctx context.Context, rf *runstore.RunFolder, w io.Writer, opts ViewOptions) error {
	if opts.Poll <= 0 {
		opts.Poll = 400 * time.Millisecond
	}
	info, err := rf.Read()
	if err != nil {
		return err
	}
	label := info.Label
	if label == "" {
		label = "Delegation run"
	}
	fmt.Fprintf(w, "%s\n\n", label)
	v := &viewer{rf: rf, w: w}
	v.colour, v.width = styleMode(w, opts)
	for {
		info, err := rf.Read()
		if err != nil {
			return err
		}
		v.step(info.Finished)
		v.asks(opts.Asks)
		if info.Finished {
			return v.finish(info)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(opts.Poll):
		}
	}
}

type viewer struct {
	rf  *runstore.RunFolder
	w   io.Writer
	src string // the file followed
	ask string // the asks last printed
	off int64
	doc docReader

	colour bool       // style lines: a terminal, NO_COLOR unset
	width  func() int // the terminal width, read per line
	sty    lineStyle
}

// asks prints the pending asks when they changed.
func (v *viewer) asks(list func() []string) {
	if list == nil {
		return
	}
	cur := list()
	key := strings.Join(cur, "\n")
	if key == v.ask {
		return
	}
	v.ask = key
	for _, a := range cur {
		fmt.Fprintf(v.w, "[pending] %s\n", a)
	}
}

// step prints what is new; final takes a trailing partial line too.
func (v *viewer) step(final bool) {
	if v.src != runstore.FileCleanMD {
		if _, _, err := v.rf.ReadFrom(runstore.FileCleanMD, 0); err == nil {
			if v.off > 0 {
				fmt.Fprintln(v.w, "--- clean.md ---")
			}
			v.src, v.off, v.doc = runstore.FileCleanMD, 0, docReader{}
			v.sty.reset()
		} else {
			v.src = runstore.FileCleanLog
		}
	}
	data, size, err := v.rf.ReadFrom(v.src, v.off)
	if err != nil {
		return
	}
	if size < v.off { // replaced by something shorter: start over
		fmt.Fprintf(v.w, "--- %s was rewritten ---\n", v.src)
		v.off, v.doc = 0, docReader{}
		v.sty.reset()
		data, _, _ = v.rf.ReadFrom(v.src, 0)
	}
	if !final {
		data = data[:bytes.LastIndexByte(data, '\n')+1]
	}
	v.off += int64(len(data))
	for _, l := range splitLines(data) {
		v.print(l)
	}
	if final && v.src == runstore.FileCleanMD {
		if p, ok := v.doc.flush(); ok {
			v.printFold(p)
		}
	}
}

func (v *viewer) print(l string) {
	if v.src != runstore.FileCleanMD {
		fmt.Fprintln(v.w, l)
		return
	}
	switch kind, p := v.doc.line(l); kind {
	case lineProse:
		fmt.Fprintln(v.w, v.sty.style(l, v.width(), v.colour))
	case lineFoldEnd:
		v.printFold(p)
	}
}

var (
	fenceLineRe = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	headLineRe  = regexp.MustCompile(`^#{1,6} `)
)

// printFold is one line per tool result; a failed one shows its output
// (without fences, its label and its error quote) indented under it.
func (v *viewer) printFold(p Part) {
	if !p.Failed {
		fmt.Fprintf(v.w, "%s\n", v.mark("✓", styles.ok, p.Title))
		return
	}
	fmt.Fprintf(v.w, "%s\n", v.mark("✗", styles.fail, p.Title))
	for _, l := range strings.Split(p.Body, "\n") {
		if trimSpace(l) == "" || fenceLineRe.MatchString(l) || headLineRe.MatchString(l) ||
			strings.HasPrefix(l, "> **error**") || labelRe.MatchString(l) {
			continue
		}
		fmt.Fprintf(v.w, "    %s\n", l)
	}
}

// finish prints the status and the answer, or the end of clean.log.
func (v *viewer) finish(info runstore.RunInfo) error {
	v.sty.reset() // an unclosed fence ends with the run
	fmt.Fprintf(v.w, "\n%s\n", Status(statusOf(info)))
	last, _, err := v.rf.ReadFrom(runstore.FileLastMD, 0)
	if answer := trimSpace(string(last)); err == nil && answer != "" {
		_, err = fmt.Fprintf(v.w, "\n--- answer ---\n%s\n", answer)
		return err
	}
	tail, err := v.rf.Tail(runstore.FileCleanLog, tailAnswer)
	if log := trimSpace(string(tail)); err == nil && log != "" {
		_, err = fmt.Fprintf(v.w, "\n--- end of clean.log ---\n%s\n", log)
		return err
	}
	_, err = fmt.Fprintln(v.w, "\n(no answer was written)")
	return err
}

// mark is a fold's ✓ or ✗ and title; the mark is coloured when styling.
func (v *viewer) mark(sym string, st lipgloss.Style, title string) string {
	if !v.colour {
		return sym + " " + title
	}
	return st.Render(sym) + " " + title
}

// styleMode decides whether lines are styled and how the width is read.
func styleMode(w io.Writer, opts ViewOptions) (bool, func() int) {
	f, isFile := w.(*os.File)
	colour := isFile && term.IsTerminal(f.Fd()) && os.Getenv("NO_COLOR") == ""
	if opts.Color != nil {
		colour = *opts.Color
	}
	width := opts.Width
	if width == nil {
		width = func() int {
			if !isFile {
				return 0
			}
			cols, _, err := term.GetSize(f.Fd())
			if err != nil {
				return 0
			}
			return cols
		}
	}
	return colour, width
}
