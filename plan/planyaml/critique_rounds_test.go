package planyaml

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func critiqueFixture() *PlanCritique {
	return &PlanCritique{
		ReviewedAt: "2026-11-18",
		Critics: []CriticSection{{
			Title:  "Design critic",
			Status: "completed",
			Sections: []CriticSubsection{{
				Heading: "Scope",
				Items: []CriticItem{
					{Badge: "S1", Kind: "warn", Text: "Slice 2 has no rollback step"},
					{Badge: "S2", Kind: "danger", Text: "Done-when line 3 cannot be tested"},
					{Badge: "S3", Kind: "info", Text: "Naming is consistent"},
				},
			}},
		}},
	}
}

// An accepted risk is stored with its reason and a later round does not
// raise it again.
func TestCritique_AcceptedRiskStaysSettled(t *testing.T) {
	prev := critiqueFixture()
	prev.Round = 1
	prev.AcceptedRisks = []AcceptedRisk{{Text: "Slice 2 has no rollback step", Reason: "Owner accepted on 2026-11-18"}}

	next := critiqueFixture() // the later round raises the same finding again
	next.AcceptedRisks = nil
	SettleCritique(prev, next)

	if len(next.AcceptedRisks) != 1 || next.AcceptedRisks[0].Reason != "Owner accepted on 2026-11-18" {
		t.Fatalf("accepted risk not carried forward: %+v", next.AcceptedRisks)
	}
	for _, c := range next.Critics {
		for _, s := range c.Sections {
			for _, it := range s.Items {
				if it.Text == "Slice 2 has no rollback step" {
					t.Errorf("settled finding raised again: %+v", it)
				}
			}
		}
	}
	open := OpenFindings(next)
	if len(open) != 1 || open[0].Text != "Done-when line 3 cannot be tested" {
		t.Errorf("open findings = %+v, want only the danger item", open)
	}

	// The risk survives a YAML round trip.
	data, err := yaml.Marshal(prev)
	if err != nil {
		t.Fatal(err)
	}
	var back PlanCritique
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.AcceptedRisks) != 1 || back.AcceptedRisks[0].Text != "Slice 2 has no rollback step" {
		t.Errorf("round trip lost accepted risk: %+v", back.AcceptedRisks)
	}
}

// Each critic records completed or not_run, and nothing else.
func TestCritique_CriticStatusRecorded(t *testing.T) {
	c := critiqueFixture()
	c.Critics = append(c.Critics, CriticSection{Title: "Feasibility critic", Status: "not_run"})
	if errs := ValidateCritique(c); len(errs) != 0 {
		t.Fatalf("valid statuses refused: %v", errs)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "status: not_run") || !strings.Contains(string(data), "status: completed") {
		t.Errorf("yaml does not record both statuses:\n%s", data)
	}

	c.Critics[1].Status = "skipped"
	errs := ValidateCritique(c)
	if len(errs) != 1 || !strings.Contains(errs[0], `critique.critics[1].status "skipped"`) {
		t.Errorf("errs = %v, want one error naming critics[1].status", errs)
	}
}

// A contested finding needs a path:line or an http(s) URL.
func TestCritique_ContestedFindingNeedsCitation(t *testing.T) {
	good := []string{"cmd/wipnote/plan_critique.go:42", "https://example.com/docs", "http://example.com/x"}
	bad := []string{"", "see the code", "plan_critique.go", "cmd/wipnote/plan_critique.go:", "ftp://example.com/x", "file.go:abc"}
	for _, cite := range good {
		c := critiqueFixture()
		c.Critics[0].Sections[0].Items[0].Contested = true
		c.Critics[0].Sections[0].Items[0].Citation = cite
		if errs := ValidateCritique(c); len(errs) != 0 {
			t.Errorf("citation %q refused: %v", cite, errs)
		}
	}
	for _, cite := range bad {
		c := critiqueFixture()
		c.Critics[0].Sections[0].Items[0].Contested = true
		c.Critics[0].Sections[0].Items[0].Citation = cite
		errs := ValidateCritique(c)
		if len(errs) != 1 || !strings.Contains(errs[0], "contested") || !strings.Contains(errs[0], "path:line or http(s) URL") {
			t.Errorf("citation %q: errs = %v, want one contested-citation error", cite, errs)
		}
	}
	// A finding that is not contested needs no citation.
	c := critiqueFixture()
	if errs := ValidateCritique(c); len(errs) != 0 {
		t.Errorf("uncontested finding refused: %v", errs)
	}
}
