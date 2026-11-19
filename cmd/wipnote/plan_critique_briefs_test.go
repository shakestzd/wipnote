package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/plan/planyaml"
)

// briefPlan makes a temp project with a plan that has a drafting trail: a
// critic revision, a prior critique and feedback. It returns the dir and id.
func briefPlan(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	id, err := createPlanFromTopic(dir, "Brief Plan", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"Alpha", "Beta", "Gamma"} {
		if err := runPlanAddSliceYAML(dir, id, s, "Do "+s, "", "", "", "", "S", "Low", ""); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "plans", id+".yaml")
	plan, err := planyaml.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	plan.Slices[0].Files = []string{"cmd/wipnote/alpha.go", "cmd/wipnote/alpha_new.go (new)"}
	plan.Slices[1].Files = []string{"cmd/wipnote/alpha.go", "plan/planyaml/beta.go"}
	plan.Slices[0].CriticRevisions = []planyaml.CriticRevision{{Source: "OLD-DRAFT-MARKER", Summary: "OLD-DRAFT-MARKER"}}
	if err := planyaml.Save(path, plan); err != nil {
		t.Fatal(err)
	}
	return dir, id
}

func TestPlanCritique_WritesTwoBriefs_NoDraftHistory(t *testing.T) {
	dir, id := briefPlan(t)
	out, err := runPlanCritiqueBriefs(dir, id)
	if err != nil {
		t.Fatalf("critique: %v", err)
	}
	if out.PlanID != id || out.SliceCount != 3 {
		t.Errorf("existing JSON fields changed: %+v", out)
	}
	if out.Briefs == nil || len(out.Spawn) != 2 {
		t.Fatalf("want two briefs and two spawn lines, got %+v / %v", out.Briefs, out.Spawn)
	}
	design := readFile(t, out.Briefs.Design)
	feas := readFile(t, out.Briefs.Feasibility)
	for name, text := range map[string]string{"design": design, "feasibility": feas} {
		if !strings.Contains(text, id) {
			t.Errorf("%s brief lacks the plan id", name)
		}
		if !strings.Contains(text, "plans/"+id+".yaml") || !strings.Contains(text, "cmd/wipnote/alpha.go") {
			t.Errorf("%s brief lacks the reading list", name)
		}
		if strings.Contains(text, "OLD-DRAFT-MARKER") || strings.Contains(text, "critic_revisions") {
			t.Errorf("%s brief carries drafting history", name)
		}
	}
	if strings.Count(feas, "cmd/wipnote/alpha.go") != 1 {
		t.Errorf("reading list repeats a file:\n%s", feas)
	}
	// Each brief ends in its own spawn line on the review profile.
	for i, kind := range []string{"design", "feasibility"} {
		line := out.Spawn[i]
		for _, want := range []string{"wipnote delegate spawn", "--profile review", "--brief-file", kind} {
			if !strings.Contains(line, want) {
				t.Errorf("spawn line %d lacks %q: %s", i, want, line)
			}
		}
	}
	// The design brief asks for a paraphrase first.
	if !strings.Contains(design, "Paraphrase first") {
		t.Errorf("design brief does not ask for a paraphrase first")
	}
	// The feasibility brief lists the three added checks.
	for _, want := range []string{"Existing helper", "Design choice left to the implementer", "New files"} {
		if !strings.Contains(feas, want) {
			t.Errorf("feasibility brief lacks the %q check", want)
		}
	}
}

func TestPlanCritique_ThirdRoundRefused(t *testing.T) {
	dir, id := briefPlan(t)
	data := `{"reviewed_at":"2026-11-18","critics":[{"title":"Design","status":"completed","sections":[{"heading":"Scope","items":[{"badge":"D1","kind":"danger","text":"Slice 2 has no done-when line"}]}]}]}`
	for i := 0; i < 2; i++ {
		if _, err := runPlanCritiqueBriefs(dir, id); err != nil {
			t.Fatalf("round %d refused: %v", i+1, err)
		}
		if err := setCritiqueYAML(dir, id, data); err != nil {
			t.Fatalf("record round %d: %v", i+1, err)
		}
	}
	plan, _ := planyaml.Load(filepath.Join(dir, "plans", id+".yaml"))
	if plan.Critique == nil || plan.Critique.Round != 2 {
		t.Fatalf("round = %+v, want 2", plan.Critique)
	}
	_, err := runPlanCritiqueBriefs(dir, id)
	if err == nil {
		t.Fatal("third round was not refused")
	}
	for _, want := range []string{"capped at 2 rounds", "Slice 2 has no done-when line"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal lacks %q: %v", want, err)
		}
	}
}

func TestSetCritique_AcceptedRiskCarriesAndContestedNeedsCitation(t *testing.T) {
	dir, id := briefPlan(t)
	first := `{"reviewed_at":"2026-11-18","accepted_risks":[{"text":"No rollback step","reason":"Owner accepted"}],"critics":[]}`
	if err := setCritiqueYAML(dir, id, first); err != nil {
		t.Fatal(err)
	}
	second := `{"reviewed_at":"2026-11-19","critics":[{"title":"Design","status":"not_run","sections":[{"heading":"S","items":[{"badge":"D1","kind":"warn","text":"No rollback step"}]}]}]}`
	if err := setCritiqueYAML(dir, id, second); err != nil {
		t.Fatal(err)
	}
	plan, _ := planyaml.Load(filepath.Join(dir, "plans", id+".yaml"))
	c := plan.Critique
	if len(c.AcceptedRisks) != 1 || len(c.Critics[0].Sections[0].Items) != 0 || c.Critics[0].Status != "not_run" {
		t.Errorf("accepted risk not settled or status lost: %+v", c)
	}
	bad := `{"reviewed_at":"2026-11-19","critics":[{"title":"D","sections":[{"heading":"S","items":[{"badge":"D1","kind":"warn","text":"x","contested":true}]}]}]}`
	err := setCritiqueYAML(dir, id, bad)
	if err == nil || !strings.Contains(err.Error(), "path:line or http(s) URL") {
		t.Errorf("contested finding without citation not refused: %v", err)
	}
}

func TestPlanNextCritique_NamesPlanCritiqueCommand(t *testing.T) {
	text := planNextRules["Critique"]
	if !strings.Contains(text, "wipnote plan critique <plan-id>") || strings.Contains(text, "/wipnote:"+"plan-critique") {
		t.Errorf("Critique step text = %q", text)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSetCritique_ThirdWriteRefusedRoundStaysTwo(t *testing.T) {
	dir, id := briefPlan(t)
	data := `{"reviewed_at":"2026-11-18","critics":[{"title":"Design","status":"completed","sections":[{"heading":"Scope","items":[{"badge":"D1","kind":"danger","text":"Slice 2 has no done-when line"}]}]}]}`
	for i := 0; i < 2; i++ {
		if err := setCritiqueYAML(dir, id, data); err != nil {
			t.Fatalf("write %d: %v", i+1, err)
		}
	}
	err := setCritiqueYAML(dir, id, data)
	if err == nil || !strings.Contains(err.Error(), "capped at 2 rounds") {
		t.Fatalf("third write error = %v, want the cap message", err)
	}
	plan, _ := planyaml.Load(filepath.Join(dir, "plans", id+".yaml"))
	if plan.Critique == nil || plan.Critique.Round != 2 {
		t.Errorf("stored round = %+v, want 2", plan.Critique)
	}
}

func TestSetCritique_EmptyCriticStatusRefusedNamingCritic(t *testing.T) {
	dir, id := briefPlan(t)
	data := `{"reviewed_at":"2026-11-18","critics":[{"title":"Feasibility critic","status":"","sections":[]}]}`
	err := setCritiqueYAML(dir, id, data)
	if err == nil || !strings.Contains(err.Error(), "Feasibility critic") || !strings.Contains(err.Error(), "status") {
		t.Fatalf("error = %v, want a refusal naming Feasibility critic and status", err)
	}
	plan, _ := planyaml.Load(filepath.Join(dir, "plans", id+".yaml"))
	if plan.Critique != nil {
		t.Errorf("critique stored despite refusal: %+v", plan.Critique)
	}
}
