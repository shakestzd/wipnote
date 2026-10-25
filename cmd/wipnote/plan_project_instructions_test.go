package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

const projectInstructionsFixture = `meta:
    id: plan-brieffix
    title: Brief fixture plan
    description: Ship the brief export.
    status: finalized
    version: 2
design:
    problem: |
        ## Non-goals

        - A web dashboard.
    goals:
        - One command prints the brief.
    constraints:
        - No host paths in output.
slices:
    - id: slic-1
      num: 1
      title: Export the brief
      feature_id: feat-fix1
      files:
        - plan/projectbrief/brief.go
      done_when:
        - Output is at most 16000 characters
      tests: 'Unit: TestBrief_UnderLimit.'
      approved: true
questions: []
`

func TestPlanProjectInstructions_PrintsBrief(t *testing.T) {
	// t.Setenv restores TestMain's sandbox value after the test; the unset
	// keeps the explicit wipnoteDir the only project source here.
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	dir := filepath.Join(t.TempDir(), ".wipnote")
	if err := os.MkdirAll(filepath.Join(dir, "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plans", "plan-brieffix.yaml"), []byte(projectInstructionsFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := runPlanProjectInstructions(&buf, dir, "plan-brieffix"); err != nil {
		t.Fatalf("runPlanProjectInstructions: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Brief fixture plan", "## What this project is for", "## What needs the owner's go-ahead first", "feat-fix1", "A web dashboard."} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q", want)
		}
	}
	if utf8.RuneCountInString(out) > 16000 {
		t.Errorf("output over the limit")
	}
	var again bytes.Buffer
	_ = runPlanProjectInstructions(&again, dir, "plan-brieffix")
	if again.String() != out {
		t.Errorf("second run differs")
	}
	if err := runPlanProjectInstructions(&bytes.Buffer{}, dir, "plan-missing"); err == nil {
		t.Fatal("want an error for a missing plan")
	}
}

func TestPlanProjectInstructions_Registered(t *testing.T) {
	cmd := planCmdWithExtras()
	sub, _, err := cmd.Find([]string{"project-instructions"})
	if err != nil || sub == nil || sub.Name() != "project-instructions" {
		t.Fatalf("plan project-instructions not registered: %v", err)
	}
}
