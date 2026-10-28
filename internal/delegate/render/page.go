package render

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// MDToHTML renders the Markdown subset; fences follow the pane's rule
// (fenceStep), so the page and the viewers agree on every block.
func MDToHTML(src string) string {
	lines := strings.Split(strings.ReplaceAll(src, "\r", ""), "\n")
	var out []string
	for i := 0; i < len(lines); {
		html, next := htmlBlock(lines, i)
		if html != "" {
			out = append(out, html)
		}
		i = next
	}
	return strings.Join(out, "\n")
}

// htmlBlock renders the block starting at line i and returns the next line.
func htmlBlock(lines []string, i int) (string, int) {
	l := lines[i]
	if fence := fenceStep(nil, l); fence != nil {
		var body []string
		for i++; i < len(lines); i++ {
			if fence = fenceStep(fence, lines[i]); fence == nil {
				break
			}
			body = append(body, lines[i])
		}
		return "<pre>" + escHTML(strings.Join(body, "\n")) + "</pre>", i + 1
	}
	if h := headingRe.FindStringSubmatch(l); h != nil {
		if h[2] == "Turn" { // the Codex turn marker says nothing on a page
			return "", i + 1
		}
		return "<h3>" + inline(h[2]) + "</h3>", i + 1
	}
	if strings.HasPrefix(l, "> ") {
		return "<blockquote>" + inline(l[2:]) + "</blockquote>", i + 1
	}
	if listRe.MatchString(l) {
		var items []string
		for ; i < len(lines) && listRe.MatchString(lines[i]); i++ {
			items = append(items, "<li>"+inline(lines[i][2:])+"</li>")
		}
		return "<ul>" + strings.Join(items, "") + "</ul>", i
	}
	if trimSpace(l) == "" {
		return "", i + 1
	}
	var para []string
	for ; i < len(lines) && trimSpace(lines[i]) != "" && fenceStep(nil, lines[i]) == nil && !blockRe.MatchString(lines[i]); i++ {
		para = append(para, lines[i])
	}
	return "<p>" + inline(strings.Join(para, " ")) + "</p>", i
}

// PageInput is what run.html shows.
type PageInput struct {
	Label   string
	Status  string        // "finished in 22s · input ..."
	Group   session.Group // the run's Overview group (slice 25)
	OK      bool
	Parts   []Part // clean.md, ParseRun(text, true)
	Answer  string
	LogPath string
}

// Page is a finished run as one HTML page: the answer first when the run
// succeeded, then the run with each tool result in a <details> fold (open
// when it failed). Everything from the run is escaped and its free text has
// credentials masked (runstore.Redact, linear) before it is rendered: the
// page is a file that is sent on.
func Page(p PageInput) string {
	var live []string
	for _, part := range p.Parts {
		if part.Kind == PartProse {
			live = append(live, "<section>"+MDToHTML(runstore.Redact(part.Text))+"</section>")
			continue
		}
		attr := ""
		if part.Failed {
			attr = ` class="failed" open`
		}
		live = append(live, "<details"+attr+"><summary>"+escHTML(runstore.Redact(part.Title))+"</summary>"+
			MDToHTML(runstore.Redact(part.Body))+"</details>")
	}
	liveHTML := strings.Join(live, "\n")
	if liveHTML == "" {
		liveHTML = "<p><em>No events were logged.</em></p>"
	}
	answer := "<p><em>No answer was written; see Live.</em></p>"
	first, state := "live", "--bad"
	if trimSpace(p.Answer) != "" {
		answer = MDToHTML(runstore.Redact(p.Answer))
		if p.OK {
			first = "answer"
		}
	}
	if p.OK {
		state = "--ok"
	}
	return fmt.Sprintf(pageHTML, pageCSS, first, state, escHTML(runstore.Redact(p.Label)), groupHTML(p.Group),
		escHTML(runstore.Redact(p.Status)), liveHTML, answer, escHTML(p.LogPath), pageScript)
}

// groupHTML is the group's tag before the status line, or "" for no group.
func groupHTML(g session.Group) string {
	if g.Label() == "" {
		return ""
	}
	return `<span class="group" data-group="` + escHTML(string(g)) + `">` + escHTML(g.Label()) + `</span> · `
}

// StatusInput is how far a run got.
type StatusInput struct {
	Running bool
	Seconds int64 // run time so far, or in all
	Exit    int
	Usage   string
}

// Status is the status line the viewer and the page show.
func Status(s StatusInput) string {
	took := clock(s.Seconds)
	if s.Running {
		return "running for " + took
	}
	out := "finished"
	if s.Exit != 0 {
		out = fmt.Sprintf("failed with exit %d", s.Exit)
	}
	out += " in " + took
	if s.Usage != "" {
		out += " · " + s.Usage
	}
	return out
}

// clock is 22s or 1m05s.
func clock(sec int64) string {
	sec = max(sec, 0)
	if m := sec / 60; m > 0 {
		return fmt.Sprintf("%dm%02ds", m, sec%60)
	}
	return fmt.Sprintf("%ds", sec)
}

// maxAnswer bounds the answer shown on the page, as the mod does.
const maxAnswer = 20000

// WritePage writes run.html into a finished run's folder from its
// clean.md, last.md and status files, and returns its path.
func WritePage(rf *runstore.RunFolder) (string, error) {
	info, err := rf.Read()
	if err != nil {
		return "", err
	}
	md, _, err := rf.ReadFrom(runstore.FileCleanMD, 0)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	last, _, err := rf.ReadFrom(runstore.FileLastMD, 0)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	group, _ := session.GroupOf(session.RunStateOf(!info.Finished, &info.Exit, ""))
	page := Page(PageInput{
		Label: info.Label, Status: Status(statusOf(info)), OK: info.Finished && info.Exit == 0, Group: group,
		Parts: ParseRun(string(md), true), Answer: firstRunes(trimSpace(string(last)), maxAnswer),
		LogPath: filepath.Join(rf.Dir(), runstore.FileCleanMD),
	})
	if err := rf.WriteFile(runstore.FileRunHTML, []byte(page)); err != nil {
		return "", err
	}
	return filepath.Join(rf.Dir(), runstore.FileRunHTML), nil
}

// statusOf is a run folder's status, its time counted to now while live.
func statusOf(info runstore.RunInfo) StatusInput {
	end := info.End
	if end.IsZero() {
		end = time.Now()
	}
	s := StatusInput{Running: !info.Finished, Exit: info.Exit, Usage: info.Usage}
	if !info.Start.IsZero() {
		s.Seconds = int64(end.Sub(info.Start) / time.Second)
	}
	return s
}

func firstRunes(s string, n int) string {
	if runeLen(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
