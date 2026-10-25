package interview

import "testing"

func TestStagesForComplexity_StageCounts(t *testing.T) {
	cases := map[string]int{
		"trivial":  0,
		"standard": 3,
		"complex":  4,
		"":         3, // unset defaults to standard (mirrors effectiveComplexity)
		"bogus":    3, // unknown defaults to standard
	}
	for complexity, want := range cases {
		got := len(forComplexity(complexity))
		if got != want {
			t.Errorf("forComplexity(%q): got %d stages, want %d", complexity, got, want)
		}
	}
}

func TestStageBlockPlan_MapsStagesToBlockTypes(t *testing.T) {
	cases := map[string][]string{
		"scope":        {"file-tree"},
		"contract":     {"data-model"},
		"requirements": {"wireframe", "diagram"},
		"donewhen":     nil,
		"unknown":      nil,
	}
	for key, want := range cases {
		got := StageBlockPlan(key)
		if len(got) != len(want) {
			t.Errorf("StageBlockPlan(%q): got %d prompts, want %d", key, len(got), len(want))
			continue
		}
		for i, ty := range want {
			if got[i].Type != ty {
				t.Errorf("StageBlockPlan(%q)[%d].Type = %q, want %q", key, i, got[i].Type, ty)
			}
			if got[i].Prompt == "" {
				t.Errorf("StageBlockPlan(%q)[%d] has empty prompt", key, i)
			}
		}
	}
}

func TestStagesForComplexity_BucketsCoverThreeFields(t *testing.T) {
	// Complex runs all stages; every stage must target a known decisions bucket.
	valid := map[Bucket]bool{BucketScope: true, BucketDecisions: true, BucketContext: true}
	for _, st := range forComplexity("complex") {
		if !valid[st.Bucket] {
			t.Errorf("stage %q has invalid bucket %q", st.Key, st.Bucket)
		}
		if len(st.Questions) == 0 {
			t.Errorf("stage %q has no questions", st.Key)
		}
	}
}

func TestPlanIntakeStages_LeadsWithTriage(t *testing.T) {
	stages := PlanIntakeStages()
	if len(stages) == 0 || stages[0].Key != "triage" {
		t.Fatalf("intake must lead with a triage stage; got %+v", stages)
	}
	if stages[0].Questions[0].ID != QIntakeComplexity {
		t.Errorf("first question should be the complexity assessment, got %q", stages[0].Questions[0].ID)
	}
}

func TestBuildForSlice_AppendsOpenQuestions(t *testing.T) {
	base := len(forComplexity("complex"))
	open := []Question{{ID: "slicequestion.sq-1", Header: "sq-1", Prompt: "GraphQL or REST?", Type: Text}}
	stages := BuildForSlice("complex", open)
	if len(stages) != base+1 {
		t.Fatalf("got %d stages, want %d (template + 1 open-questions stage)", len(stages), base+1)
	}
	last := stages[len(stages)-1]
	if last.Key != "slice-questions" || last.Bucket != BucketDecisions || len(last.Questions) != 1 {
		t.Errorf("open-questions stage wrong: %+v", last)
	}
	// No open questions → identical to forComplexity.
	if len(BuildForSlice("standard", nil)) != len(forComplexity("standard")) {
		t.Error("BuildForSlice with no open questions should equal forComplexity")
	}
}
