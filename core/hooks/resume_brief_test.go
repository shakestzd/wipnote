package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/db"
)

const briefItemID = "feat-b01dface"

// briefItemHTML is a work item file shaped like the canonical writer's output:
// steps in an ordered list, the description in the content section, and the
// questions in the data-node-props attribute.
func briefItemHTML(id, title string, steps []string, content, questionsJSON string) string {
	var b strings.Builder
	props := ""
	if questionsJSON != "" {
		props = ` data-node-props="` + strings.ReplaceAll(questionsJSON, `"`, "&#34;") + `"`
	}
	fmt.Fprintf(&b, `<!DOCTYPE html><html><head><title>%s</title></head><body>`, title)
	fmt.Fprintf(&b, `<article id="%s" data-type="feature" data-status="in-progress"%s>`, id, props)
	fmt.Fprintf(&b, `<header><h1>%s</h1></header>`, title)
	if len(steps) > 0 {
		b.WriteString(`<section data-steps><h3>Implementation Steps</h3><ol>`)
		for i, s := range steps {
			done := "false"
			if strings.HasPrefix(s, "+") {
				done, s = "true", s[1:]
			}
			fmt.Fprintf(&b, `<li data-completed="%s" data-step-id="step-%s-%d">%s</li>`, done, id, i, s)
		}
		b.WriteString(`</ol></section>`)
	}
	if content != "" {
		fmt.Fprintf(&b, `<section data-content><h3>Description</h3><p>%s</p></section>`, content)
	}
	b.WriteString(`</article></body></html>`)
	return b.String()
}

// briefProject makes a project whose session holds an open claim on a work
// item with the given steps and content.
func briefProject(t *testing.T, steps []string, content, questionsJSON string) string {
	t.Helper()
	projectDir := canonGuardProject(t)
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	dir := filepath.Join(wipnoteDir, "features")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := briefItemHTML(briefItemID, "Keep the thread", steps, content, questionsJSON)
	if err := os.WriteFile(filepath.Join(dir, briefItemID+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
	store := claimledger.NewStore(wipnoteDir)
	if _, _, err := store.Open(canonGuardSession, claimledger.Episode{WorkItemID: briefItemID,
		SessionID: canonGuardSession, RootSessionID: canonGuardSession,
		AgentID: db.AgentRootSentinel, StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	return projectDir
}

const briefContent = "Wire the thing.\nWhy\nAfter compaction the agent loses its steps.\nAcceptance Criteria\nThe brief names the item and the next step."

const briefQuestions = `{"questions":[{"id":"q-1","text":"Which cap do we use?","options":[],"status":"open"},{"id":"q-2","text":"Already settled","options":[],"status":"confirmed"}]}`

func TestResumeBrief_WithActiveItem(t *testing.T) {
	projectDir := briefProject(t, []string{"+Read the docs", "Write the builder", "Wire the hook"}, briefContent, briefQuestions)
	got := BuildResumeBrief(projectDir, canonGuardSession)

	for _, want := range []string{
		briefItemID, "Keep the thread",
		"After compaction the agent loses its steps.",
		"The brief names the item and the next step.",
		"- [x] Read the docs", "- [ ] Write the builder", "- [ ] Wire the hook",
		"Which cap do we use?",
		"Next step: Write the builder",
		"Run `wipnote context-pack " + briefItemID + "` for the full brief.",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("brief missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Already settled") {
		t.Errorf("a confirmed question must not appear:\n%s", got)
	}
	if len(got) >= resumeBriefMaxChars {
		t.Errorf("brief is %d chars, want under %d", len(got), resumeBriefMaxChars)
	}
}

func TestResumeBrief_NoActiveItem(t *testing.T) {
	projectDir := canonGuardProject(t)
	if got := BuildResumeBrief(projectDir, canonGuardSession); got != "" {
		t.Fatalf("no claim: want empty brief, got %q", got)
	}
}

func TestResumeBrief_OmitsSectionsWithoutData(t *testing.T) {
	projectDir := briefProject(t, nil, "", "")
	got := BuildResumeBrief(projectDir, canonGuardSession)
	if !strings.Contains(got, briefItemID) || !strings.Contains(got, "wipnote context-pack") {
		t.Fatalf("a bare item still gets its id and the full-brief pointer:\n%s", got)
	}
	for _, heading := range []string{"Why", "Done when", "Steps", "Open questions", "Next step"} {
		if strings.Contains(got, heading) {
			t.Errorf("section %q has no data and must be left out:\n%s", heading, got)
		}
	}
}

func TestResumeBrief_StaysUnderCap(t *testing.T) {
	long := strings.Repeat("a very long sentence about nothing in particular ", 40)
	var steps []string
	for i := 0; i < 60; i++ {
		steps = append(steps, fmt.Sprintf("Step %d: %s", i, long))
	}
	content := "Why\n" + long + "\nAcceptance Criteria\n" + long
	var qs []string
	for i := 0; i < 20; i++ {
		qs = append(qs, fmt.Sprintf(`{"id":"q-%d","text":"%s","options":[],"status":"open"}`, i, long))
	}
	projectDir := briefProject(t, steps, content, `{"questions":[`+strings.Join(qs, ",")+`]}`)

	got := BuildResumeBrief(projectDir, canonGuardSession)
	if len(got) >= resumeBriefMaxChars {
		t.Fatalf("brief is %d chars, want under %d", len(got), resumeBriefMaxChars)
	}
	if !strings.Contains(got, "wipnote context-pack "+briefItemID) {
		t.Errorf("the pointer to the full brief must survive the cap:\n%s", got)
	}
	if !strings.Contains(got, "Next step: Step 0") {
		t.Errorf("the next step must survive the cap:\n%s", got)
	}
}
