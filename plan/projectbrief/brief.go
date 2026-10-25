// Package projectbrief turns a wipnote plan into the text a Claude project
// takes as its project instructions (Project settings > Memory > Project
// instructions). The plan YAML stays canonical: the brief is a printed copy
// and nothing reads it back.
//
// The sections follow the brief shape in
// https://code.claude.com/docs/en/claude-projects ("Write project
// instructions"): what the project is for, where the work happens, how a
// thread checks its work, what to do when something is missing, and what
// needs the owner's go-ahead. Project instructions hold at most 16,000
// characters, so Build trims list items and names every item it cut.
package projectbrief

import (
	"strings"
	"unicode/utf8"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// Limit is the project-instructions character limit.
const Limit = 16000

const trimNoteLead = "Cut to fit the 16,000-character limit"

// Brief is a rendered project-instructions text. Cut names each list whose
// items were dropped to fit Limit, with the dropped item names; it is empty
// when nothing was cut.
type Brief struct {
	Text string
	Cut  []string
}

// item is one bullet in a list; name identifies it in the trim note.
type item struct{ name, text string }

// list is a labelled bullet list. Lower rank is cut first, and lists of equal
// rank shrink together (the longer one first); within a list the last item
// goes first, so a plan's later slices are cut before earlier ones.
type list struct {
	label string
	rank  int
	items []item
	cut   []string
}

type section struct {
	title string
	lead  []string
	lists []*list
}

type doc struct {
	planID string
	header string
	secs   []*section
}

// Build renders the brief for plan within Limit.
func Build(plan *planyaml.PlanYAML) Brief {
	return BuildWithLimit(plan, Limit)
}

// BuildWithLimit renders the brief for plan within limit characters.
func BuildWithLimit(plan *planyaml.PlanYAML, limit int) Brief {
	d := derive(plan)
	text := d.render()
	for utf8.RuneCountInString(text) > limit && d.cutOne() {
		text = d.render()
	}
	if utf8.RuneCountInString(text) > limit {
		text, cut := hardCut(d, limit)
		return Brief{Text: text, Cut: cut}
	}
	return Brief{Text: text, Cut: d.cutLines()}
}

// cutOne drops the last item of the lowest-ranked list that still has items.
func (d *doc) cutOne() bool {
	var pick *list
	for _, s := range d.secs {
		for _, l := range s.lists {
			if len(l.items) > 0 && (pick == nil || l.rank < pick.rank ||
				(l.rank == pick.rank && len(l.items) > len(pick.items))) {
				pick = l
			}
		}
	}
	if pick == nil {
		return false
	}
	last := pick.items[len(pick.items)-1]
	pick.items = pick.items[:len(pick.items)-1]
	pick.cut = append([]string{last.name}, pick.cut...)
	return true
}

// hardCut is the last resort when the lead text alone exceeds limit: it
// keeps the head of the body and names the cut tail alongside the cut lists.
func hardCut(d *doc, limit int) (string, []string) {
	cut := append(d.cutLines(), "the text after this point")
	note := trimNote(d.planID, cut)
	keep := limit - utf8.RuneCountInString(note) - 1
	if keep < 0 {
		keep = 0
	}
	body := []rune(d.renderBody())
	if len(body) > keep {
		body = body[:keep]
	}
	return string(body) + "\n" + note, cut
}

func trimNote(planID string, cut []string) string {
	var b strings.Builder
	b.WriteString("\n" + trimNoteLead + " (the full text is in wipnote plan " + planID + "):\n")
	for _, c := range cut {
		b.WriteString("- " + c + "\n")
	}
	return b.String()
}

func (d *doc) cutLines() []string {
	var out []string
	for _, s := range d.secs {
		for _, l := range s.lists {
			if len(l.cut) > 0 {
				out = append(out, s.title+" > "+strings.TrimSuffix(l.label, ":")+": "+strings.Join(l.cut, ", "))
			}
		}
	}
	return out
}

func (d *doc) render() string {
	body := d.renderBody()
	cut := d.cutLines()
	if len(cut) == 0 {
		return body
	}
	return body + trimNote(d.planID, cut)
}

func (d *doc) renderBody() string {
	var b strings.Builder
	b.WriteString(d.header)
	for _, s := range d.secs {
		b.WriteString("## " + s.title + "\n\n")
		for _, line := range s.lead {
			b.WriteString(line + "\n\n")
		}
		for _, l := range s.lists {
			if len(l.items) == 0 {
				continue
			}
			b.WriteString(l.label + "\n")
			for _, it := range l.items {
				b.WriteString("- " + it.text + "\n")
			}
			b.WriteString("\n")
		}
	}
	return paths.SanitizeHostPaths(b.String())
}
