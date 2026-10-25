package render

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ViewOptions tunes View.
type ViewOptions struct {
	Poll time.Duration // how often the run folder is read; 400ms when zero
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
	for {
		info, err := rf.Read()
		if err != nil {
			return err
		}
		v.step(info.Finished)
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
	off int64
	doc docReader
}

// step prints what is new; final takes a trailing partial line too.
func (v *viewer) step(final bool) {
	if v.src != runstore.FileCleanMD {
		if _, _, err := v.rf.ReadFrom(runstore.FileCleanMD, 0); err == nil {
			if v.off > 0 {
				fmt.Fprintln(v.w, "--- clean.md ---")
			}
			v.src, v.off, v.doc = runstore.FileCleanMD, 0, docReader{}
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
		fmt.Fprintln(v.w, l)
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
		fmt.Fprintf(v.w, "✓ %s\n", p.Title)
		return
	}
	fmt.Fprintf(v.w, "✗ %s\n", p.Title)
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
