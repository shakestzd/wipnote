package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/plan/planyaml"
)

// nextFixture writes a plan to a temp store and returns the store dir.
// mutate edits the plan before it is saved.
func nextFixture(t *testing.T, mutate func(p *planyaml.PlanYAML)) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := planyaml.NewPlan("plan-next01", "Fix a typo", "Small plan")
	p.Design = planyaml.PlanDesign{
		Problem: "A typo.",
		Goals:   []string{"No typo"}, Constraints: []string{"Keep the fix small"},
		Research: []planyaml.ResearchSource{
			{URL: "https://example.com/docs", Claim: "Docs confirm the spelling.", Accessed: "2026-11-18"},
		},
	}
	p.Slices = []planyaml.PlanSlice{{
		ID: "slice-1", Num: 1, Title: "Fix the typo", Why: "It reads wrong.",
		Files: []string{"a.go"}, Effort: "S", Risk: "Low", Complexity: "trivial",
	}}
	mutate(p)
	if err := planyaml.Save(filepath.Join(dir, "plans", "plan-next01.yaml"), p); err != nil {
		t.Fatal(err)
	}
	return dir, "plan-next01"
}

func nextOutput(t *testing.T, dir, id string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runPlanNext(&buf, dir, id); err != nil {
		t.Fatalf("runPlanNext: %v", err)
	}
	return buf.String()
}

func wantStep(t *testing.T, out, step string) {
	t.Helper()
	first := strings.SplitN(out, "\n", 2)[0]
	if !strings.HasPrefix(first, "Next step: "+step) {
		t.Errorf("first line = %q, want prefix %q\n%s", first, "Next step: "+step, out)
	}
}

func approvedSlice(p *planyaml.PlanYAML) {
	p.Slices[0].ApprovalStatus = "approved"
	p.Slices[0].Approved = true
}

func TestPlanNext_Draft(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) { p.Slices = nil })
	wantStep(t, nextOutput(t, dir, id), "Draft")
}

func TestPlanNext_Validate(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) { p.Slices[0].Effort = "huge" })
	wantStep(t, nextOutput(t, dir, id), "Validate")
}

func TestPlanNext_Critique(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {
		for n := 2; n <= 3; n++ {
			s := p.Slices[0]
			s.ID, s.Num = "slice-"+string(rune('0'+n)), n
			p.Slices = append(p.Slices, s)
		}
	})
	wantStep(t, nextOutput(t, dir, id), "Critique")
}

func TestPlanNext_Review(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {})
	wantStep(t, nextOutput(t, dir, id), "Review")
}

func TestPlanNext_Decisions(t *testing.T) {
	dir, id := nextFixture(t, approvedSlice)
	wantStep(t, nextOutput(t, dir, id), "Decisions")
}

func TestPlanNext_Promote(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {
		approvedSlice(p)
		p.Slices[0].DecisionsNotes = "**Decisions:** fix the spelling in place."
	})
	wantStep(t, nextOutput(t, dir, id), "Promote")
}

// A slice whose dependency is not done cannot be promoted, so it is not offered.
func TestPlanNext_PromoteSkipsBlockedSlice(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {
		approvedSlice(p)
		p.Slices[0].DecisionsNotes = "**Decisions:** fix the spelling in place."
		p.Slices[0].FeatureID = "feat-aaaa1111"
		p.Slices[0].ExecutionStatus = "promoted"
		blocked := p.Slices[0]
		blocked.Num = p.Slices[0].Num + 1
		blocked.ID = ""
		blocked.FeatureID = ""
		blocked.ExecutionStatus = ""
		blocked.Deps = []int{p.Slices[0].Num}
		p.Slices = append(p.Slices, blocked)
	})
	out := nextOutput(t, dir, id)
	if strings.HasPrefix(out, "Next step: Promote") {
		t.Fatalf("a slice with an unfinished dependency was offered for promotion:\n%s", out)
	}
}

func TestPlanNext_Spec(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {
		approvedSlice(p)
		p.Slices[0].DecisionsNotes = "**Decisions:** fix the spelling in place."
		p.Slices[0].FeatureID = "feat-aaaa1111"
		p.Slices[0].ExecutionStatus = "promoted"
	})
	wantStep(t, nextOutput(t, dir, id), "Spec")
}

func TestPlanNext_Close(t *testing.T) {
	dir, id := nextFixture(t, func(p *planyaml.PlanYAML) {
		approvedSlice(p)
		p.Slices[0].FeatureID = "feat-aaaa1111"
		p.Slices[0].ExecutionStatus = "done"
	})
	wantStep(t, nextOutput(t, dir, id), "Close")
}

func TestPlanNext_OutputUnder40Lines(t *testing.T) {
	cases := map[string]func(p *planyaml.PlanYAML){
		"Draft":     func(p *planyaml.PlanYAML) { p.Slices = nil },
		"Validate":  func(p *planyaml.PlanYAML) { p.Slices[0].Effort = "huge" },
		"Review":    func(p *planyaml.PlanYAML) {},
		"Decisions": approvedSlice,
		"Promote": func(p *planyaml.PlanYAML) {
			approvedSlice(p)
			p.Slices[0].DecisionsNotes = "**Decisions:** fix it."
		},
		"Spec": func(p *planyaml.PlanYAML) {
			approvedSlice(p)
			p.Slices[0].DecisionsNotes = "**Decisions:** fix it."
			p.Slices[0].FeatureID = "feat-aaaa1111"
		},
		"Close": func(p *planyaml.PlanYAML) {
			approvedSlice(p)
			p.Slices[0].FeatureID = "feat-aaaa1111"
			p.Slices[0].ExecutionStatus = "done"
		},
	}
	for step, mutate := range cases {
		dir, id := nextFixture(t, mutate)
		out := nextOutput(t, dir, id)
		wantStep(t, out, step)
		if n := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; n >= 40 {
			t.Errorf("%s output has %d lines, want under 40", step, n)
		}
	}
}

func TestPlanNext_NoID(t *testing.T) {
	var buf bytes.Buffer
	if err := runPlanNext(&buf, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"trivial", "standard", "complex", "wipnote plan create-yaml", "one or two files"} {
		if !strings.Contains(out, want) {
			t.Errorf("no-id output lacks %q:\n%s", want, out)
		}
	}
	if n := strings.Count(strings.TrimRight(out, "\n"), "\n") + 1; n >= 40 {
		t.Errorf("no-id output has %d lines, want under 40", n)
	}
}
