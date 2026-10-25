package projectbrief

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/hostpath"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// Section titles, in the order the claude-projects brief lists them.
const (
	sectionPurpose = "What this project is for"
	sectionWhere   = "Where the work happens"
	sectionChecks  = "How a thread checks its work"
	sectionMissing = "When something it needs is missing"
	sectionGoAhead = "What needs the owner's go-ahead first"
)

const (
	noRepoLine      = "The plan names no repository."
	noBranchLine    = "The plan names no branch to start from."
	noPRLine        = "The plan names no pull-request naming rule."
	missingMetaLine = "Where the plan names none and the task does not say, treat it as missing (see below)."
	closedLead      = "Closed slices left out (done or superseded): "
	noGatesLine     = "The plan lists no quality-gate commands; run the ones the repository's own instructions name."
	checkLead       = "Before calling a slice done, show in your final message that each of its done-when items holds and that its tests pass."
	missingRule     = "If you cannot reach something you need, such as a repository, a branch, a secret, a tool or an answer the plan does not give, say exactly what is missing in your first message and stop. Do not substitute, mock or guess."
	noGoAheadLine   = "The plan lists no unapproved open slices, open questions or non-goals."
)

// Sections returns the brief's section titles in output order.
func Sections() []string {
	return []string{sectionPurpose, sectionWhere, sectionChecks, sectionMissing, sectionGoAhead}
}

func derive(p *planyaml.PlanYAML, opts Options) *doc {
	slices, closed := openSlices(p.Slices, opts.FeatureStatus)
	header := "# Project instructions: " + oneLine(p.Meta.Title) + "\n\n" +
		fmt.Sprintf("Exported from wipnote plan %s (version %d). The wipnote plan is canonical; this text is a copy and nothing reads it back.\n\n", p.Meta.ID, p.Meta.Version)
	return &doc{planID: p.Meta.ID, header: header, secs: []*section{
		purposeSection(p),
		whereSection(p.Meta, slices, closed),
		checksSection(p, slices),
		{title: sectionMissing, lead: []string{missingRule}},
		goAheadSection(p, slices),
	}}
}

func purposeSection(p *planyaml.PlanYAML) *section {
	s := &section{title: sectionPurpose}
	if d := strings.TrimSpace(p.Meta.Description); d != "" {
		s.lead = append(s.lead, d)
	} else {
		s.lead = append(s.lead, "The plan has no description; its title is the purpose.")
	}
	s.lists = []*list{
		textList("Goals:", 3, "goal", p.Design.Goals),
		textList("Decisions already made (rationale in the plan):", 2, "decision", mdBoldLeads(p.Design.Problem, "decision")),
	}
	return s
}

// openSlices returns the slices still open, sorted by number, and the names
// of the closed ones. A slice is closed when its execution_status is done or
// superseded, or when featureStatus reports its linked work item as done.
func openSlices(all []planyaml.PlanSlice, featureStatus func(string) string) ([]planyaml.PlanSlice, []string) {
	sorted := append([]planyaml.PlanSlice(nil), all...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Num < sorted[j].Num })
	var open []planyaml.PlanSlice
	var closed []string
	for _, sl := range sorted {
		done := sl.ExecutionStatus == "done" || sl.ExecutionStatus == "superseded"
		if !done && featureStatus != nil && sl.FeatureID != "" {
			done = featureStatus(sl.FeatureID) == "done"
		}
		if done {
			closed = append(closed, fmt.Sprintf("slice %d", sl.Num))
		} else {
			open = append(open, sl)
		}
	}
	return open, closed
}

// whereLead states the plan's repositories, start branch and pull-request
// naming, with a "names no" line for each one the plan leaves out.
func whereLead(m planyaml.PlanMeta) []string {
	var lead []string
	missing := false
	add := func(ok bool, line, none string) {
		if ok {
			lead = append(lead, line)
		} else {
			lead, missing = append(lead, none), true
		}
	}
	add(len(m.Repositories) > 0, "Repositories: "+joinLines(m.Repositories, ", ")+".", noRepoLine)
	add(oneLine(m.BaseBranch) != "", "Start from branch `"+oneLine(m.BaseBranch)+"`.", noBranchLine)
	add(oneLine(m.PRNaming) != "", "Pull-request naming: "+oneLine(m.PRNaming), noPRLine)
	if missing {
		lead = append(lead, missingMetaLine)
	}
	return lead
}

func whereSection(m planyaml.PlanMeta, slices []planyaml.PlanSlice, closed []string) *section {
	areas := &list{label: "Code areas the slices touch:", rank: 2}
	for _, a := range codeAreas(slices) {
		areas.items = append(areas.items, item{name: a, text: a})
	}
	work := &list{label: "Slices and their work items:", rank: 0}
	for _, sl := range slices {
		text := fmt.Sprintf("Slice %d (%s): %s", sl.Num, sliceState(sl), oneLine(sl.Title))
		if len(sl.Files) > 0 {
			text += ". Files: " + strings.Join(sl.Files, ", ")
		}
		work.items = append(work.items, item{name: fmt.Sprintf("slice %d", sl.Num), text: text})
	}
	lead := whereLead(m)
	if len(closed) > 0 {
		lead = append(lead, closedLead+strings.Join(closed, ", ")+".")
	}
	return &section{title: sectionWhere, lead: lead, lists: []*list{areas, work}}
}

func checksSection(p *planyaml.PlanYAML, slices []planyaml.PlanSlice) *section {
	checks := &list{label: "Slice checks:", rank: 0}
	for _, sl := range slices {
		var parts []string
		if len(sl.DoneWhen) > 0 {
			parts = append(parts, "done when: "+joinLines(sl.DoneWhen, "; "))
		}
		if t := oneLine(sl.Tests); t != "" {
			parts = append(parts, "tests: "+t)
		}
		if len(parts) == 0 {
			parts = append(parts, "the plan gives no done-when items or tests")
		}
		checks.items = append(checks.items, item{
			name: fmt.Sprintf("slice %d", sl.Num),
			text: fmt.Sprintf("Slice %d: %s", sl.Num, strings.Join(parts, ". ")),
		})
	}
	lead := []string{checkLead}
	gates := &list{label: "Quality gates to run before calling work done:", rank: 7}
	for i, g := range p.Meta.Gates {
		if g = oneLine(g); g != "" {
			gates.items = append(gates.items, item{name: fmt.Sprintf("gate %d", i+1), text: "`" + g + "`"})
		}
	}
	if len(gates.items) == 0 {
		lead = append(lead, noGatesLine)
	}
	return &section{title: sectionChecks, lead: lead, lists: []*list{
		gates,
		textList("Constraints every change keeps:", 4, "constraint", p.Design.Constraints),
		checks,
	}}
}

func goAheadSection(p *planyaml.PlanYAML, slices []planyaml.PlanSlice) *section {
	pending := &list{label: "Slices not yet approved (do not start them):", rank: 6}
	questions := &list{label: "Open questions (ask the owner; do not pick an answer):", rank: 5}
	for _, q := range p.Questions {
		if q.Answer == nil || strings.TrimSpace(*q.Answer) == "" {
			questions.items = append(questions.items, item{name: q.ID, text: q.ID + ": " + oneLine(q.Text)})
		}
	}
	for _, sl := range slices {
		if sliceApproval(sl) != "approved" {
			pending.items = append(pending.items, item{name: fmt.Sprintf("slice %d", sl.Num),
				text: fmt.Sprintf("Slice %d: %s (%s)", sl.Num, oneLine(sl.Title), sliceApproval(sl))})
		}
		for _, q := range sl.Questions {
			if strings.TrimSpace(q.Answer) == "" {
				questions.items = append(questions.items, item{name: q.ID,
					text: fmt.Sprintf("Slice %d, %s: %s", sl.Num, q.ID, oneLine(q.Text))})
			}
		}
	}
	nonGoals := textList("Out of scope (the plan's non-goals):", 3, "non-goal", mdBullets(p.Design.Problem, "non-goal"))
	s := &section{title: sectionGoAhead, lists: []*list{pending, questions, nonGoals}}
	if len(pending.items)+len(questions.items)+len(nonGoals.items) == 0 {
		s.lead = []string{noGoAheadLine}
	}
	return s
}

// textList builds a list of one-line items named "<kind> N".
func textList(label string, rank int, kind string, texts []string) *list {
	l := &list{label: label, rank: rank}
	for i, t := range texts {
		if t = oneLine(t); t != "" {
			l.items = append(l.items, item{name: fmt.Sprintf("%s %d", kind, i+1), text: t})
		}
	}
	return l
}

func sliceApproval(sl planyaml.PlanSlice) string {
	if sl.ApprovalStatus != "" {
		return sl.ApprovalStatus
	}
	if sl.Approved {
		return "approved"
	}
	return "pending"
}

func sliceState(sl planyaml.PlanSlice) string {
	parts := []string{}
	if sl.FeatureID != "" {
		parts = append(parts, sl.FeatureID)
	} else {
		parts = append(parts, "no work item yet")
	}
	parts = append(parts, sliceApproval(sl))
	if sl.ExecutionStatus != "" {
		parts = append(parts, sl.ExecutionStatus)
	}
	return strings.Join(parts, ", ")
}

// codeAreas returns the sorted, de-duplicated directories the slices' files
// sit in (two levels deep), skipping host-local and other absolute paths.
func codeAreas(slices []planyaml.PlanSlice) []string {
	seen := map[string]bool{}
	for _, sl := range slices {
		for _, f := range sl.Files {
			f = hostpath.Redact(f)
			if f == "" || strings.HasPrefix(f, "/") || strings.HasPrefix(f, "[host-path]") {
				continue
			}
			parts := strings.Split(f, "/")
			switch {
			case len(parts) >= 3:
				f = parts[0] + "/" + parts[1]
			case len(parts) == 2:
				f = parts[0]
			}
			seen[f] = true
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

var (
	boldLeadRe = regexp.MustCompile(`^\s*(?:\d+\.|-)\s+\*\*(.+?)\*\*`)
	bulletRe   = regexp.MustCompile(`^\s*-\s+(.+)$`)
)

// mdBoldLeads returns the bold lead of each list item under markdown
// headings whose text contains match (case-insensitive).
func mdBoldLeads(md, match string) []string {
	var out []string
	for _, line := range mdSectionLines(md, match) {
		if m := boldLeadRe.FindStringSubmatch(line); m != nil {
			out = append(out, strings.TrimRight(strings.TrimSpace(m[1]), ".:"))
		}
	}
	return out
}

// mdBullets returns the "- " list items under headings containing match.
func mdBullets(md, match string) []string {
	var out []string
	for _, line := range mdSectionLines(md, match) {
		if m := bulletRe.FindStringSubmatch(line); m != nil {
			out = append(out, m[1])
		}
	}
	return out
}

func mdSectionLines(md, match string) []string {
	var out []string
	in := false
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			in = strings.Contains(strings.ToLower(line), match)
			continue
		}
		if in {
			out = append(out, line)
		}
	}
	return out
}

func joinLines(texts []string, sep string) string {
	out := make([]string, 0, len(texts))
	for _, t := range texts {
		if t = oneLine(t); t != "" {
			out = append(out, t)
		}
	}
	return strings.Join(out, sep)
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
