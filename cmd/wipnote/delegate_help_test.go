package main

import (
	"strings"
	"testing"
)

// delegate --help carries the steps the retired delegation guidance held
// (feat-8f910039), in under 40 lines.
func TestDelegateHelp_Under40Lines(t *testing.T) {
	out, err := runCLI(t, "delegate", "--help")
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Split(strings.TrimRight(out, "\n"), "\n")); n >= 40 {
		t.Errorf("delegate --help is %d lines, want under 40:\n%s", n, out)
	}
	last := -1
	for _, step := range []string{"delegate spawn", "delegate send", "delegate wait", "delegate result", "delegate stop"} {
		i := strings.Index(out, step)
		if i < 0 || i < last {
			t.Fatalf("step %q missing or out of order in:\n%s", step, out)
		}
		last = i
	}
	for _, want := range []string{"delegate profiles", "delegate run", "complete"} {
		if !strings.Contains(out, want) {
			t.Errorf("delegate --help lacks %q", want)
		}
	}
}

// delegate profiles prints each profile, review included, from the session map.
func TestDelegateProfiles_ListsReview(t *testing.T) {
	out, err := runCLI(t, "delegate", "profiles")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"simple", "standard", "deep", "full", "review", "haiku", "sonnet", "opus", "NotebookEdit", "read-only"} {
		if !strings.Contains(out, want) {
			t.Errorf("delegate profiles lacks %q:\n%s", want, out)
		}
	}
	var reviewLine string
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "review") {
			reviewLine = l
		}
	}
	cols := strings.Fields(reviewLine) // name, model, effort, tools
	if len(cols) < 4 || cols[1] != "sonnet" || cols[2] != "medium" || strings.Contains(cols[3], "Edit") || strings.Contains(cols[3], "Write") {
		t.Errorf("review line = %q", reviewLine)
	}
}

// spawn prints the next command, so the caller need not read a skill.
func TestSpawnNextStepHint(t *testing.T) {
	got := spawnNextHint("api-fix")
	for _, want := range []string{"wipnote delegate wait api-fix --timeout", "wipnote delegate result api-fix", "wipnote delegate stop api-fix"} {
		if !strings.Contains(got, want) {
			t.Errorf("hint %q lacks %q", got, want)
		}
	}
}
