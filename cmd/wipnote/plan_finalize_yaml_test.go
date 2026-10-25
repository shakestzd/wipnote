package main

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/plan/planyaml"
)

func TestApplyAmendments(t *testing.T) {
	plan := &planyaml.PlanYAML{
		Slices: []planyaml.PlanSlice{
			{Num: 1, Title: "Original", DoneWhen: []string{"existing"}, Files: []string{"a.go"}, Effort: "S", Risk: "Low"},
			{Num: 2, Title: "Slice 2", DoneWhen: []string{"criterion"}, Effort: "M"},
		},
	}

	amendments := []planAmendment{
		{SliceNum: 1, Field: "done_when", Operation: "add", Content: "new criterion"},
		{SliceNum: 1, Field: "title", Operation: "set", Content: "Updated Title"},
		{SliceNum: 1, Field: "files", Operation: "add", Content: "b.go"},
		{SliceNum: 2, Field: "effort", Operation: "set", Content: "L"},
		{SliceNum: 2, Field: "done_when", Operation: "remove", Content: "criterion"},
		{SliceNum: 99, Field: "title", Operation: "set", Content: "Missing"}, // should be skipped
	}

	applyAmendments(plan, amendments)

	// Verify slice 1
	if plan.Slices[0].Title != "Updated Title" {
		t.Errorf("slice 1 title: got %q, want %q", plan.Slices[0].Title, "Updated Title")
	}
	if len(plan.Slices[0].DoneWhen) != 2 {
		t.Errorf("slice 1 done_when len: got %d, want 2", len(plan.Slices[0].DoneWhen))
	} else if plan.Slices[0].DoneWhen[1] != "new criterion" {
		t.Errorf("slice 1 done_when[1]: got %q, want %q", plan.Slices[0].DoneWhen[1], "new criterion")
	}
	if len(plan.Slices[0].Files) != 2 {
		t.Errorf("slice 1 files len: got %d, want 2", len(plan.Slices[0].Files))
	} else if plan.Slices[0].Files[1] != "b.go" {
		t.Errorf("slice 1 files[1]: got %q, want %q", plan.Slices[0].Files[1], "b.go")
	}

	// Verify slice 2
	if plan.Slices[1].Effort != "L" {
		t.Errorf("slice 2 effort: got %q, want %q", plan.Slices[1].Effort, "L")
	}
	if len(plan.Slices[1].DoneWhen) != 0 {
		t.Errorf("slice 2 done_when len: got %d, want 0 (criterion removed)", len(plan.Slices[1].DoneWhen))
	}
}

func TestApplyAmendments_SetScalars(t *testing.T) {
	plan := &planyaml.PlanYAML{
		Slices: []planyaml.PlanSlice{
			{Num: 3, Title: "Old", What: "old what", Why: "old why", Risk: "Low"},
		},
	}

	amendments := []planAmendment{
		{SliceNum: 3, Field: "what", Operation: "set", Content: "new what"},
		{SliceNum: 3, Field: "why", Operation: "set", Content: "new why"},
		{SliceNum: 3, Field: "risk", Operation: "set", Content: "High"},
	}

	applyAmendments(plan, amendments)

	if plan.Slices[0].What != "new what" {
		t.Errorf("what: got %q, want %q", plan.Slices[0].What, "new what")
	}
	if plan.Slices[0].Why != "new why" {
		t.Errorf("why: got %q, want %q", plan.Slices[0].Why, "new why")
	}
	if plan.Slices[0].Risk != "High" {
		t.Errorf("risk: got %q, want %q", plan.Slices[0].Risk, "High")
	}
}

func TestRemoveStr(t *testing.T) {
	result := removeStr([]string{"a", "b", "c"}, "b")
	if len(result) != 2 || result[0] != "a" || result[1] != "c" {
		t.Errorf("removeStr: got %v, want [a c]", result)
	}

	// Remove non-existent element — slice unchanged
	result2 := removeStr([]string{"x"}, "y")
	if len(result2) != 1 || result2[0] != "x" {
		t.Errorf("removeStr non-existent: got %v, want [x]", result2)
	}

	// Remove from empty slice
	result3 := removeStr([]string{}, "z")
	if len(result3) != 0 {
		t.Errorf("removeStr empty: got %v, want []", result3)
	}

	// Remove duplicate occurrences
	result4 := removeStr([]string{"a", "b", "a"}, "a")
	if len(result4) != 1 || result4[0] != "b" {
		t.Errorf("removeStr duplicates: got %v, want [b]", result4)
	}
}

func TestBuildFeatureContent_NoQuestions(t *testing.T) {
	content := buildFeatureContent("base what", nil, nil)
	if content != "base what" {
		t.Errorf("no questions: got %q, want %q", content, "base what")
	}
}

func TestBuildFeatureContent_WithAnswers(t *testing.T) {
	questions := []planyaml.PlanQuestion{
		{
			ID:          "q1",
			Text:        "Caching strategy",
			Recommended: "lazy",
			Options: []planyaml.QuestionOption{
				{Key: "lazy", Label: "Lazy loading"},
				{Key: "eager", Label: "Eager loading"},
			},
		},
		{
			ID:          "q2",
			Text:        "Error handling",
			Recommended: "structured-log",
			Options: []planyaml.QuestionOption{
				{Key: "structured-log", Label: "Structured log"},
				{Key: "metric-counter", Label: "Metric counter"},
			},
		},
	}
	// Human answered q2 with metric-counter; q1 falls back to recommended.
	answers := map[string]string{"q2": "metric-counter"}

	content := buildFeatureContent("do the thing", questions, answers)

	if !strings.Contains(content, "## Accepted Design Decisions") {
		t.Error("expected Accepted Design Decisions section")
	}
	if !strings.Contains(content, "Lazy loading") {
		t.Errorf("expected q1 fallback label 'Lazy loading' in %q", content)
	}
	if !strings.Contains(content, "Metric counter") {
		t.Errorf("expected q2 answer label 'Metric counter' in %q", content)
	}
	if !strings.Contains(content, "do the thing") {
		t.Error("expected base 'what' text to be preserved")
	}
}

func TestBuildFeatureContent_FallbackToRecommended(t *testing.T) {
	questions := []planyaml.PlanQuestion{
		{
			ID:          "q1",
			Text:        "Storage backend",
			Recommended: "sqlite",
			Options: []planyaml.QuestionOption{
				{Key: "sqlite", Label: "SQLite"},
				{Key: "postgres", Label: "PostgreSQL"},
			},
		},
	}
	// No answers provided — should fall back to recommended.
	content := buildFeatureContent("store data", questions, map[string]string{})

	if !strings.Contains(content, "SQLite") {
		t.Errorf("expected fallback label 'SQLite' in %q", content)
	}
	if !strings.Contains(content, "unanswered") {
		t.Errorf("expected 'unanswered' marker for fallback in %q", content)
	}
}

func TestBuildFeatureContent_SkipsQuestionsWithNoAnswer(t *testing.T) {
	// Question with no recommended and no answer should be skipped.
	questions := []planyaml.PlanQuestion{
		{ID: "q1", Text: "Empty question", Options: []planyaml.QuestionOption{
			{Key: "a", Label: "Option A"},
		}},
	}
	content := buildFeatureContent("base", questions, map[string]string{})
	if strings.Contains(content, "Accepted Design Decisions") {
		t.Error("should not emit decisions section when no question has an answer or recommended")
	}
}

// ---------------------------------------------------------------------------
// Regression tests for bug-3e524d0f: finalize-yaml must read plan_feedback
// approvals and reconcile them into the YAML before creating track+features.
// ---------------------------------------------------------------------------
