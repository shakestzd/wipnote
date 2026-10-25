package projectbrief

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shakestzd/wipnote/core/hostpath"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

func TestMain(m *testing.M) {
	os.Unsetenv("WIPNOTE_PROJECT_DIR")
	os.Unsetenv("CLAUDE_PROJECT_DIR")
	os.Exit(m.Run())
}

const problemFixture = `## Problem

Two paths exist.

## Non-goals

- A web dashboard.
- Remote machines over SSH.

## Design decisions (rationale in each slice)

1. **No daemon.** Every verb is a short process.
2. **One writer per file.** Temp file plus rename.

## Owner decisions

- **R. Result:** the worker writes a result file.

## Migration

- **orch users.** Not a decision.
`

func smallPlan() *planyaml.PlanYAML {
	open := ""
	answered := "yes"
	return &planyaml.PlanYAML{
		Meta: planyaml.PlanMeta{ID: "plan-fixture", Title: "Fixture plan", Description: "Make delegation native.", Version: 3},
		Design: planyaml.PlanDesign{
			Problem:     problemFixture,
			Goals:       []string{"One native path."},
			Constraints: []string{"No daemon dependency.", "Code limits apply."},
		},
		Slices: []planyaml.PlanSlice{
			{Num: 2, Title: "Second", FeatureID: "feat-b", Files: []string{"internal/delegate/run.go"},
				DoneWhen: []string{"Run exits zero"}, Tests: "Unit: TestRun.", ApprovalStatus: "pending"},
			{Num: 1, Title: "First", FeatureID: "feat-a", Files: []string{"cmd/wipnote/a.go", "go.mod"},
				DoneWhen: []string{"A prints"}, Tests: "Unit: TestA.", Approved: true, ApprovalStatus: "approved",
				Questions: []planyaml.SliceQuestion{{ID: "sq-1", Text: "Which flag name?"}}},
		},
		Questions: []planyaml.PlanQuestion{
			{ID: "q-open", Text: "Adopt or port?", Answer: &open},
			{ID: "q-done", Text: "Command name?", Answer: &answered},
		},
	}
}

func largePlan(n int) *planyaml.PlanYAML {
	p := smallPlan()
	p.Slices = nil
	for i := 1; i <= n; i++ {
		p.Slices = append(p.Slices, planyaml.PlanSlice{
			Num: i, Title: fmt.Sprintf("Slice title %d", i), FeatureID: fmt.Sprintf("feat-%04d", i),
			Files:    []string{fmt.Sprintf("internal/area%d/file.go", i)},
			DoneWhen: []string{fmt.Sprintf("SENTINEL-%d-%s", i, strings.Repeat("x", 120))},
			Tests:    "Unit: TestSomething.", Approved: true, ApprovalStatus: "approved",
		})
	}
	return p
}

func TestBrief_UnderLimit(t *testing.T) {
	small := Build(smallPlan())
	if len(small.Cut) != 0 || strings.Contains(small.Text, trimNoteLead) {
		t.Fatalf("small plan was trimmed: cut=%v", small.Cut)
	}
	big := Build(largePlan(200))
	if n := utf8.RuneCountInString(big.Text); n > Limit {
		t.Fatalf("brief is %d characters, limit %d", n, Limit)
	}
	if n := utf8.RuneCountInString(big.Text); n < Limit/2 {
		t.Fatalf("brief trimmed far below the limit: %d characters", n)
	}
	huge := smallPlan()
	huge.Meta.Description = strings.Repeat("é", Limit+500)
	hb := Build(huge)
	if n := utf8.RuneCountInString(hb.Text); n > Limit || !strings.Contains(hb.Text, trimNoteLead) {
		t.Fatalf("oversized lead text: %d characters, note present=%v", n, strings.Contains(hb.Text, trimNoteLead))
	}
	if !utf8.ValidString(hb.Text) || !strings.Contains(hb.Text, "the text after this point") {
		t.Fatalf("hard cut did not name the cut tail")
	}
}

func TestBrief_TrimNamesCutSections(t *testing.T) {
	big := Build(largePlan(200))
	if len(big.Cut) == 0 || !strings.Contains(big.Text, trimNoteLead) {
		t.Fatalf("large plan not marked as trimmed")
	}
	if strings.Contains(big.Text, "SENTINEL-200-") {
		t.Fatalf("slice 200's checks survived the trim")
	}
	if !strings.Contains(big.Text, "SENTINEL-1-") {
		t.Fatalf("slice 1's checks were cut before later slices")
	}
	if !strings.Contains(big.Text, sectionChecks) || !strings.Contains(big.Text, "slice 200") {
		t.Fatalf("trim note does not name the cut section and items:\n%s", tail(big.Text, 600))
	}
	for _, c := range big.Cut {
		if !strings.Contains(big.Text, c) {
			t.Fatalf("cut entry %q not named in the output", c)
		}
	}
}

func TestBrief_NoHostPaths(t *testing.T) {
	p := smallPlan()
	p.Meta.Description = "See /Users/alice/proj/notes.md for context."
	p.Design.Constraints = append(p.Design.Constraints, "Never write to /home/bob/cache/x.")
	p.Slices[0].Files = append(p.Slices[0].Files, "/workspaces/wipnote/internal/x.go")
	input := p.Meta.Description + p.Design.Constraints[2] + p.Slices[0].Files[1]
	if len(hostpath.Find(input)) != 3 {
		t.Fatalf("fixture should carry three host paths")
	}
	out := Build(p).Text
	if found := hostpath.Find(out); len(found) != 0 {
		t.Fatalf("brief holds host paths %v", found)
	}
	if !strings.Contains(out, "notes.md") {
		t.Fatalf("redaction dropped the portable remainder of the path")
	}
}

func TestBrief_Deterministic(t *testing.T) {
	a := Build(smallPlan()).Text
	b := Build(smallPlan()).Text
	if a != b {
		t.Fatalf("same plan gave different output")
	}
	p := smallPlan()
	p.Slices[0], p.Slices[1] = p.Slices[1], p.Slices[0]
	if Build(p).Text != a {
		t.Fatalf("slice order in the YAML changed the output")
	}
	big1, big2 := Build(largePlan(150)), Build(largePlan(150))
	if big1.Text != big2.Text {
		t.Fatalf("trimmed output is not deterministic")
	}
}

func TestBrief_SectionOrder(t *testing.T) {
	out := Build(smallPlan()).Text
	last := -1
	for _, h := range Sections() {
		head := "## " + h
		if strings.Count(out, head) != 1 {
			t.Fatalf("heading %q appears %d times", head, strings.Count(out, head))
		}
		i := strings.Index(out, head)
		if i <= last {
			t.Fatalf("heading %q out of order", head)
		}
		last = i
	}
	want := []string{sectionPurpose, sectionWhere, sectionChecks, sectionMissing, sectionGoAhead}
	if strings.Join(Sections(), "|") != strings.Join(want, "|") {
		t.Fatalf("Sections() = %v", Sections())
	}
}

func TestBrief_DerivesFromPlan(t *testing.T) {
	out := Build(smallPlan()).Text
	for _, want := range []string{
		"Fixture plan", "Make delegation native.", "One native path.",
		"No daemon", "One writer per file", "R. Result",
		"cmd/wipnote", "internal/delegate", "feat-a", "Run exits zero", "Unit: TestA.",
		"No daemon dependency.", "A web dashboard.", "Slice 2", "q-open", "sq-1",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief misses %q", want)
		}
	}
	for _, absent := range []string{"orch users", "q-done", "Command name?"} {
		if strings.Contains(out, absent) {
			t.Errorf("brief should not hold %q", absent)
		}
	}
}

func TestBrief_SaysWhenPlanHasNoData(t *testing.T) {
	out := Build(&planyaml.PlanYAML{Meta: planyaml.PlanMeta{ID: "plan-empty", Title: "Empty"}}).Text
	for _, want := range []string{noRepoLine, noGoAheadLine, missingRule} {
		if !strings.Contains(out, want) {
			t.Errorf("empty plan brief misses %q", want)
		}
	}
	if strings.Contains(Build(smallPlan()).Text, noGoAheadLine) {
		t.Errorf("a plan with open items should not say nothing needs a go-ahead")
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
