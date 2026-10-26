package main

import (
	"fmt"
	"io"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// writeBaseRelease prints the base CI line with its failing workflows, the
// release line, and any notes from reads that failed.
func writeBaseRelease(w io.Writer, s *gitwatch.Snapshot) {
	if b := s.Base; b != nil {
		fmt.Fprintf(w, "base %s: %s\n", b.Name, b.State)
		for _, f := range b.Failing {
			fmt.Fprintf(w, "  failed   %s (%s)  %s\n", f.Workflow, f.Conclusion, f.URL)
		}
	}
	if s.Release != nil {
		fmt.Fprintln(w, releaseLine(s.Release))
	}
	for _, n := range s.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

// releaseLine is "release <tag> (<date>), N commits since · <workflow>: <state>".
func releaseLine(r *gitwatch.Release) string {
	if r.State == gitwatch.ReleaseNone {
		return "release: none"
	}
	line := "release " + r.Tag
	if r.PublishedAt != nil {
		line += " (" + r.PublishedAt.Format("2006-01-02") + ")"
	}
	if r.CommitsSince != nil {
		line += fmt.Sprintf(", %d commits since", *r.CommitsSince)
	} else {
		line += ", commits since unknown"
	}
	if r.Workflow != nil {
		line += fmt.Sprintf(" · %s: %s", r.Workflow.Name, r.Workflow.State)
	}
	return line
}
