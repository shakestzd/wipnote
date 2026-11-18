package session

import (
	"reflect"
	"strings"
	"testing"
)

// flagValue is the word after flag in argv, or "".
func flagValue(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// The review profile is read-only (feat-8f910039): a Claude worker has no
// edit, write or notebook tool, even when --tool asks for one.
func TestReviewProfile_NoWriteTools(t *testing.T) {
	o := SpawnOptions{Harness: HarnessClaude, Profile: "review", Tools: []string{"Edit", "NotebookEdit"}}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	tools := strings.Split(flagValue(o.Flags, "--tools"), ",")
	for _, banned := range []string{"Edit", "Write", "NotebookEdit"} {
		for _, got := range tools {
			if got == banned {
				t.Errorf("--tools %v still has %s", tools, banned)
			}
		}
	}
	if len(tools) == 0 || tools[0] == "" {
		t.Fatalf("review worker has no tools at all: %v", o.Flags)
	}
	if got, want := flagValue(o.Flags, "--disallowedTools"), "Edit,Write,NotebookEdit"; got != want {
		t.Errorf("--disallowedTools = %q, want %q", got, want)
	}
}

// A Codex review worker runs in the read-only sandbox.
func TestReviewProfile_CodexReadOnly(t *testing.T) {
	o := SpawnOptions{Harness: HarnessCodex, Profile: "review"}
	if err := harnessFlags(ctxBG, &o); err != nil {
		t.Fatal(err)
	}
	if want := []string{"--sandbox", "read-only"}; !reflect.DeepEqual(o.Flags, want) {
		t.Errorf("codex review flags = %q, want %q", o.Flags, want)
	}
}

// The table delegate profiles prints comes from the code's own map.
func TestProfileTable_ListsBuiltIns(t *testing.T) {
	rows := ClaudeConfig{}.ProfileTable()
	var names []string
	for _, r := range rows {
		names = append(names, r.Name)
	}
	if want := []string{"deep", "full", "review", "simple", "standard"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	for _, r := range rows {
		if r.Name == "review" && (r.Model != "sonnet" || r.Effort != "medium") {
			t.Errorf("review row = %+v", r)
		}
	}
}
