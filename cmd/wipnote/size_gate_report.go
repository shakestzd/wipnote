package main

import (
	"fmt"
	"io"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/slug"
)

// designReviewQuestions is the review prompt from GitHub #312. It is the one
// copy: `wipnote review` prints it and the code-quality skill points here.
var designReviewQuestions = []string{
	"What design decision does each changed module hide? Name it in one sentence.",
	"If that decision changes, how many modules change with it? More than one means it leaks.",
	"Do two places now encode one rule? Derive one from the other, or have one check the other.",
}

// writeSizeReport prints the changed-file size findings. It prints nothing
// when no changed unit is over a trigger.
func writeSizeReport(w io.Writer, r sizeReport) {
	if len(r.Findings) == 0 {
		return
	}
	fmt.Fprintf(w, "\nSize review triggers (changed code vs %s; module %d, function %d lines):\n",
		slug.Truncate(r.Base, 12), hooks.ModuleLimitLines, hooks.FuncLimitLines)
	for _, f := range r.Findings {
		fmt.Fprintf(w, "  %-4s %s — %s\n", sizeFindingLevel(f), sizeFindingTarget(f), sizeFindingNote(f))
	}
}

func sizeFindingLevel(f sizeFinding) string {
	switch {
	case f.Legacy:
		return "note"
	case f.Reason != "":
		return "ok"
	}
	return "FAIL"
}

func sizeFindingTarget(f sizeFinding) string {
	if f.Name == "" {
		return fmt.Sprintf("%s: %d lines (trigger %d)", f.Path, f.Lines, f.Trigger)
	}
	return fmt.Sprintf("%s:%d func %s: %d lines (trigger %d)", f.Path, f.Line, f.Name, f.Lines, f.Trigger)
}

func sizeFindingNote(f sizeFinding) string {
	switch {
	case f.Reason != "":
		return "kept whole: " + f.Reason
	case f.Legacy:
		return fmt.Sprintf("already over the trigger at base (%d lines); not blocking", f.BaseLines)
	case f.Name == "":
		return fmt.Sprintf("check it for more than one reason to change; split it, or record `%s: <reason>` in its first %d lines", hooks.SizeOKMarker, hooks.SizeOKFileWindow)
	}
	return fmt.Sprintf("check it for more than one reason to change; split it, or record `%s: <reason>` in the comment above it", hooks.SizeOKMarker)
}

// writeDupEvidence prints repeated blocks as evidence, never as a failure.
func writeDupEvidence(w io.Writer, pairs []dupPair) {
	if len(pairs) == 0 {
		return
	}
	fmt.Fprintln(w, "\nDuplication evidence (advisory): added code that repeats code elsewhere.")
	fmt.Fprintln(w, "  If the copies must change together, derive one from the other or have one check the other; if they only look alike, leave them.")
	for _, p := range pairs {
		fmt.Fprintf(w, "  %s:%d repeats %s:%d (~%d lines)\n", p.Path, p.Line, p.OtherPath, p.OtherLine, p.Lines)
	}
}

func writeDesignQuestions(w io.Writer) {
	fmt.Fprintln(w, "\nDesign review — answer for each changed module:")
	for _, q := range designReviewQuestions {
		fmt.Fprintln(w, "  - "+q)
	}
}
