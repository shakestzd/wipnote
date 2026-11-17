package hooks

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/question"
)

// resumeBriefMaxChars is the ceiling for the resume brief. SessionStart context
// is capped well above this (core/hooks/context_budget.go); the brief stays
// small so it re-orients an agent without crowding the fresh summary.
const resumeBriefMaxChars = 2500

// resumeState is what the brief says about the active work item.
type resumeState struct {
	ID        string
	Title     string
	Why       string
	DoneWhen  string
	Steps     []resumeStep
	Questions []string
}

type resumeStep struct {
	Text string
	Done bool
}

// nextOpenStep is the first step not yet done, or "".
func (s *resumeState) nextOpenStep() string {
	for _, st := range s.Steps {
		if !st.Done {
			return st.Text
		}
	}
	return ""
}

// loadResumeState reads the work item the session family holds a claim on,
// resolved the way UserPrompt resolves its ACTIVE line. It returns nil when
// there is no active item.
func loadResumeState(projectDir, sessionID string) *resumeState {
	id := canonicalFamilyActiveWorkItem(projectDir, sessionID)
	if id == "" {
		return nil
	}
	st := &resumeState{ID: id}
	node := canonicalWorkItemNode(projectDir, id)
	if node == nil {
		return st
	}
	st.Title = node.Title
	st.Why = contentSection(node.Content, "why")
	st.DoneWhen = contentSection(node.Content, "acceptance criteria", "done when", "done-when")
	for _, s := range node.Steps {
		st.Steps = append(st.Steps, resumeStep{Text: oneLine(s.Description), Done: s.Completed})
	}
	for _, q := range question.FromNode(node) {
		if q.IsOpen() {
			st.Questions = append(st.Questions, oneLine(q.Text))
		}
	}
	return st
}

// BuildResumeBrief is the short markdown an agent needs after its context was
// compacted: the active work item, why it exists, when it is done, where the
// steps stand and what to do next. It is empty when the session has no active
// item, and always under resumeBriefMaxChars.
func BuildResumeBrief(projectDir, sessionID string) string {
	st := loadResumeState(projectDir, sessionID)
	if st == nil {
		return ""
	}
	return renderResumeBrief(st)
}

// briefLimits are the per-part ceilings renderResumeBrief shrinks until the
// brief fits.
type briefLimits struct {
	text, steps, questions int
}

// renderResumeBrief renders st, tightening the per-part limits until the
// result fits the ceiling. The id, the next step and the pointer to the full
// brief are never dropped.
func renderResumeBrief(st *resumeState) string {
	limits := briefLimits{text: 400, steps: 14, questions: 5}
	for {
		out := renderBriefWith(st, limits)
		if len(out) < resumeBriefMaxChars || limits.text <= 40 {
			if len(out) >= resumeBriefMaxChars {
				out = clip(out, resumeBriefMaxChars-1)
			}
			return out
		}
		limits = briefLimits{text: limits.text * 2 / 3, steps: max(limits.steps-2, 3), questions: max(limits.questions-1, 1)}
	}
}

func renderBriefWith(st *resumeState, l briefLimits) string {
	var b strings.Builder
	head := "# Resume: " + st.ID
	if st.Title != "" {
		head += " — " + clip(st.Title, 120)
	}
	b.WriteString(head + "\n")
	b.WriteString("Your context was compacted. Keep working on this item; this is your current assignment.\n")
	if st.Why != "" {
		fmt.Fprintf(&b, "\n**Why:** %s\n", clip(st.Why, l.text))
	}
	if st.DoneWhen != "" {
		fmt.Fprintf(&b, "\n**Done when:** %s\n", clip(st.DoneWhen, l.text))
	}
	writeBriefSteps(&b, st.Steps, l)
	if len(st.Questions) > 0 {
		b.WriteString("\n**Open questions:**\n")
		for i, q := range st.Questions {
			if i == l.questions {
				fmt.Fprintf(&b, "- … %d more\n", len(st.Questions)-i)
				break
			}
			fmt.Fprintf(&b, "- %s\n", clip(q, l.text/2))
		}
	}
	if next := st.nextOpenStep(); next != "" {
		fmt.Fprintf(&b, "\nNext step: %s\n", clip(next, l.text/2))
	}
	fmt.Fprintf(&b, "\nRun `wipnote context-pack %s` for the full brief.", st.ID)
	return b.String()
}

// writeBriefSteps lists the steps, keeping every open step ahead of finished
// ones when the limit bites, and says how many it left out.
func writeBriefSteps(b *strings.Builder, steps []resumeStep, l briefLimits) {
	if len(steps) == 0 {
		return
	}
	b.WriteString("\n**Steps:**\n")
	shown, omitted := 0, 0
	for _, wantDone := range []bool{false, true} {
		for _, s := range steps {
			if s.Done != wantDone {
				continue
			}
			if shown >= l.steps {
				omitted++
				continue
			}
			mark := " "
			if s.Done {
				mark = "x"
			}
			fmt.Fprintf(b, "- [%s] %s\n", mark, clip(s.Text, l.text/3))
			shown++
		}
	}
	if omitted > 0 {
		fmt.Fprintf(b, "- … %d more steps\n", omitted)
	}
}

// contentSection returns the text under the first heading in content that
// matches one of names (case-insensitive, optional leading #s and trailing
// colon), up to the next heading, collapsed to one line. Work item content is
// plain text with "Why" and "Acceptance Criteria" headings.
func contentSection(content string, names ...string) string {
	var body []string
	in := false
	for line := range strings.SplitSeq(content, "\n") {
		if name, isHeading := headingName(line); isHeading {
			if in {
				break
			}
			for _, n := range names {
				if name == n {
					in = true
				}
			}
			continue
		}
		if in {
			body = append(body, line)
		}
	}
	return oneLine(strings.Join(body, " "))
}

// briefHeadings are the plain-text headings work item content uses.
var briefHeadings = map[string]bool{
	"why": true, "acceptance criteria": true, "done when": true, "done-when": true,
	"description": true, "steps": true, "notes": true, "context": true,
}

// headingName reports whether line is a content heading and its lower-case name.
func headingName(line string) (string, bool) {
	t := strings.TrimSpace(line)
	hashed := strings.HasPrefix(t, "#")
	name := strings.ToLower(strings.TrimRight(strings.TrimLeft(t, "# "), ": "))
	return name, hashed || briefHeadings[name]
}

// oneLine collapses all whitespace runs to single spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip shortens s to at most n bytes of whole runes, ending in an ellipsis
// when it cut anything.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return ""
	}
	cut := n - len("…")
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return strings.TrimRight(s[:cut], " ") + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
