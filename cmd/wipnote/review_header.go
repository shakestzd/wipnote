package main

// The opening lines of `wipnote review` (feat-a1daeef8): the item's why and
// done-when, its last gate result, its linked commits and the total added
// lines. A reviewer reads these before the diff. Every field is read from what
// the store already holds, and a field the item lacks is left out.

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/gateledger"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
)

// reviewHeader is the five header fields for one work item.
type reviewHeader struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Why         string   `json:"why,omitempty"`
	DoneWhen    string   `json:"done_when,omitempty"`
	LastGate    string   `json:"last_gate,omitempty"`
	Commits     []string `json:"linked_commits,omitempty"`
	AddedLines  int      `json:"added_lines"`
	hasEvidence bool
}

var itemArtifactDirs = []string{"features", "bugs", "spikes"}

// loadItemNode reads the artifact for id, or nil when it has none.
func loadItemNode(projectRoot, id string) *models.Node {
	for _, dir := range itemArtifactDirs {
		p := filepath.Join(projectRoot, ".wipnote", dir, id+".html")
		if node, err := htmlparse.ParseFile(p); err == nil && node != nil {
			return node
		}
	}
	return nil
}

// sectionAfter returns the text under a heading line in a description, up to
// the next heading line. `wipnote feature create` writes "Why" and
// "Acceptance Criteria" as plain lines inside the description.
func sectionAfter(content, heading string) string {
	lines := strings.Split(content, "\n")
	var out []string
	in := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if in && (t == "Why" || t == "Acceptance Criteria" || t == "Description") {
			break
		}
		if in {
			out = append(out, t)
		}
		if t == heading {
			in = true
		}
	}
	return strings.TrimSpace(strings.Join(out, " "))
}

// buildReviewHeader gathers the header for id. An empty id gives an empty
// header that prints nothing.
func buildReviewHeader(projectRoot, id string, ev evidenceBlock) reviewHeader {
	h := reviewHeader{ID: id, AddedLines: ev.AddedLines}
	if id == "" {
		return h
	}
	node := loadItemNode(projectRoot, id)
	if node != nil {
		h.Title = node.Title
		h.Why = sectionAfter(node.Content, "Why")
		h.DoneWhen = sectionAfter(node.Content, "Acceptance Criteria")
	}
	if rec, err := gateledger.StoreForProject(projectRoot).LatestForWorkItem(id); err == nil && rec != nil {
		h.LastGate = fmt.Sprintf("%s at %s", rec.Status, rec.CheckedAt.UTC().Format("2006-01-02 15:04Z"))
	}
	h.Commits = linkedCommitsFn(paths.CodeRoot(projectRoot), id, node)
	return h
}

// writeReviewHeader prints the header. A field with no value is skipped.
func writeReviewHeader(w io.Writer, h reviewHeader) {
	if h.ID == "" {
		return
	}
	fmt.Fprintf(w, "Work item: %s %s\n", h.ID, h.Title)
	if h.Why != "" {
		fmt.Fprintf(w, "  why: %s\n", h.Why)
	}
	if h.DoneWhen != "" {
		fmt.Fprintf(w, "  done when: %s\n", h.DoneWhen)
	}
	if h.LastGate != "" {
		fmt.Fprintf(w, "  last gate: %s\n", h.LastGate)
	}
	if len(h.Commits) > 0 {
		short := make([]string, 0, len(h.Commits))
		for _, c := range h.Commits {
			if len(c) > 7 {
				c = c[:7]
			}
			short = append(short, c)
		}
		fmt.Fprintf(w, "  linked commits: %s\n", strings.Join(short, ", "))
	}
	fmt.Fprintf(w, "  added lines: %d\n\n", h.AddedLines)
}
