package statusall

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/procsweep"
)

// Text limits: the text view is a glance; --json carries everything.
const (
	textItems = 5
	textRuns  = 3
	textWidth = 72
)

// WriteJSON writes r as indented JSON with a trailing newline.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(r)
}

// WriteText writes the compact view: a header, notes shared by every
// project, then one block per project in report order.
func WriteText(w io.Writer, r Report, now time.Time) {
	fmt.Fprintf(w, "wipnote status --all · %d project(s)\n", len(r.Projects))
	shared := sharedSources(r.Projects)
	for _, k := range sortedKeys(shared) {
		fmt.Fprintf(w, "%s: %s\n", k, shared[k])
	}
	for _, p := range r.Projects {
		fmt.Fprintln(w)
		writeProject(w, p, shared, now)
	}
}

// sharedSources are the source states that are not ok and are the same for
// every readable project (gh skipped, herdr not available): shown once.
func sharedSources(ps []Project) map[string]string {
	out := map[string]string{}
	first := true
	for _, p := range ps {
		if len(p.Sources) == 0 {
			continue
		}
		if first {
			for k, v := range p.Sources {
				if v != SourceOK && v != SourceNone {
					out[k] = v
				}
			}
			first = false
			continue
		}
		for k, v := range out {
			if p.Sources[k] != v {
				delete(out, k)
			}
		}
	}
	return out
}

func writeProject(w io.Writer, p Project, shared map[string]string, now time.Time) {
	if unread(p) {
		fmt.Fprintf(w, "%s  error: %s\n", p.Name, strings.Join(p.Errors, "; "))
		return
	}
	head := p.Name
	if t := parseStamp(p.LastActivity); !t.IsZero() {
		head += "  · active " + ago(now, t)
	}
	if p.Branch != "" {
		head += " · on " + p.Branch
	}
	fmt.Fprintln(w, head)
	writePlanLine(w, p.Plan)
	writeItems(w, p.InProgress)
	writeCommitLine(w, p.LastCommit, now)
	writePRLine(w, p.PR)
	writeAgents(w, p.Agents)
	writeRuns(w, p.Runs)
	writeOrphans(w, p.Orphans)
	for _, n := range p.NeedsYou {
		line("NEEDS YOU", n.Kind+": "+n.What)(w)
	}
	for _, k := range sortedKeys(p.Sources) {
		if v := p.Sources[k]; strings.HasPrefix(v, SourceNotAvailable) && shared[k] != v {
			line("note", k+": "+v)(w)
		}
	}
	for _, e := range p.Errors {
		line("error", e)(w)
	}
}

func line(label, text string) func(io.Writer) {
	return func(w io.Writer) { fmt.Fprintf(w, "  %-9s %s\n", label, text) }
}

func writePlanLine(w io.Writer, pl *Plan) {
	if pl == nil {
		return
	}
	text := pl.ID + " " + cut(pl.Title, 40)
	if s := pl.Slice; s != nil {
		text += fmt.Sprintf(" · slice %d %s (%s)", s.Num, cut(s.Title, 40), s.Status)
	}
	line("plan", text+fmt.Sprintf(" · %d/%d done", pl.Done, pl.Total))(w)
}

func writeItems(w io.Writer, items []Item) {
	for i, it := range items {
		if i == textItems {
			line("doing", fmt.Sprintf("(+%d more)", len(items)-textItems))(w)
			return
		}
		line("doing", it.ID+" "+cut(it.Title, textWidth))(w)
	}
}

func writeCommitLine(w io.Writer, c *Commit, now time.Time) {
	if c == nil {
		return
	}
	line("commit", shortSHA(c.SHA)+" "+cut(c.Subject, textWidth)+" · "+ago(now, parseStamp(c.At)))(w)
}

func writePRLine(w io.Writer, pr *PR) {
	if pr == nil {
		return
	}
	c := pr.Checks
	text := fmt.Sprintf("#%d %s · checks %d pass, %d fail, %d pending", pr.Number, pr.State, c.Pass, c.Fail, c.Pending)
	if len(pr.Failing) > 0 {
		text += " (fail: " + strings.Join(pr.Failing, ", ") + ")"
	}
	if pr.Review != "" {
		text += " · review " + pr.Review
	}
	if pr.Mergeable != "" {
		text += " · " + pr.Mergeable
	}
	line("pr", text)(w)
}

func writeAgents(w io.Writer, agents []Agent) {
	for _, a := range agents {
		parts := []string{a.Harness, a.State}
		if a.Pane != "" {
			parts = append(parts, a.Pane)
		}
		if a.WorkItem != "" {
			parts = append(parts, a.WorkItem+" "+cut(a.Title, 40))
		}
		text := strings.Join(nonEmpty(parts), " ")
		if a.LastLine != "" {
			text += " · \"" + cut(a.LastLine, 60) + "\""
		}
		line("agent", text)(w)
	}
}

func writeRuns(w io.Writer, runs []Run) {
	for i, r := range runs {
		if i == textRuns {
			return
		}
		line("run", cut(r.Line, textWidth+8))(w)
	}
}

func writeOrphans(w io.Writer, orphans []procsweep.Proc) {
	for _, o := range orphans {
		line("orphan", fmt.Sprintf("%d %s · stop: %s", o.PID, cut(o.Command, textWidth), o.StopHint))(w)
	}
}

// ago renders the time since t, e.g. "5m ago", "3h ago", "2d ago".
func ago(now, t time.Time) string {
	if t.IsZero() {
		return "?"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func cut(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		return string(rs[:n-1]) + "…"
	}
	return s
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func nonEmpty(parts []string) []string {
	out := parts[:0]
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
