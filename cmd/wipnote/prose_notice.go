package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/prose"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// maxProseNotes caps the notes printed per write, so a long text never floods a session.
const maxProseNotes = 5

// proseItem is one piece of text with the path a note names, such as "slices[2].why".
type proseItem struct {
	path string
	text string
	step bool
}

// adviseProse prints prose notes for the items on stderr. It never blocks and never
// changes an exit code. WIPNOTE_PROSE=off silences it, and so does a stderr that
// is /dev/null, because nobody reads that (feat-4151d2f0).
func adviseProse(items ...proseItem) {
	if proseSilenced() {
		return
	}
	writeProseAdvice(os.Stderr, items)
}

func proseSilenced() bool {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("WIPNOTE_PROSE")), "off") {
		return true
	}
	st, err := os.Stderr.Stat()
	if err != nil {
		return true
	}
	null, err := os.Stat(os.DevNull)
	return err == nil && os.SameFile(st, null)
}

func writeProseAdvice(w io.Writer, items []proseItem) {
	var lines []string
	for _, it := range items {
		for _, n := range prose.Check(it.text, prose.Options{Step: it.step}) {
			lines = append(lines, fmt.Sprintf("  ! prose %s:%d:%d  %s  %s", it.path, n.Line, n.Col, n.Rule, n.Message))
		}
	}
	for i, l := range lines {
		if i == maxProseNotes {
			fmt.Fprintf(w, "  … and %d more (wipnote prose check)\n", len(lines)-maxProseNotes)
			return
		}
		fmt.Fprintln(w, l)
	}
}

// planSliceProse lists the what, why, tests and done_when text of every slice,
// each with its path such as "slices[2].why".
func planSliceProse(plan *planyaml.PlanYAML) []proseItem {
	var items []proseItem
	if plan == nil {
		return nil
	}
	for i := range plan.Slices {
		items = append(items, oneSliceProse(plan, i)...)
	}
	return items
}

// oneSliceProse lists the prose items of the slice at index i.
func oneSliceProse(plan *planyaml.PlanYAML, i int) []proseItem {
	s := plan.Slices[i]
	p := fmt.Sprintf("slices[%d]", i)
	items := []proseItem{
		{path: p + ".what", text: s.What},
		{path: p + ".why", text: s.Why},
		{path: p + ".tests", text: s.Tests},
	}
	for j, d := range s.DoneWhen {
		items = append(items, proseItem{path: fmt.Sprintf("%s.done_when[%d]", p, j), text: d})
	}
	return items
}

// adviseSliceProse prints prose notes for the slice with the given number.
// Plan write commands call it after a save, so a changed slice gets the same
// stderr advice that validate-yaml gives (bug-c4885c55: two write points had none).
func adviseSliceProse(plan *planyaml.PlanYAML, sliceNum int) {
	for i := range plan.Slices {
		if plan.Slices[i].Num == sliceNum {
			adviseProse(oneSliceProse(plan, i)...)
			return
		}
	}
}
