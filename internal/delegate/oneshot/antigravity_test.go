package oneshot

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func TestClassify_Antigravity(t *testing.T) {
	for _, argv := range [][]string{
		{"agy", "-p", "review this"}, {"agy", "--print", "x"}, {"antigravity", "--prompt=x"},
		{"/opt/bin/agy", "--mode", "plan", "--print", "x"}, {"agy", "-p", "x", "--output-format", "text"},
	} {
		r, err := Classify(argv)
		if err != nil || r.Kind != KindAntigravity || r.Name() != "agy" {
			t.Errorf("%v: kind %q err %v", argv, r.Kind, err)
		}
		if got := r.Command(CommandOptions{}); strings.Join(got, " ") != strings.Join(argv, " ") {
			t.Errorf("%v: the argv changed to %v", argv, got)
		}
	}
	// Interactive, subcommand and unreadable-answer calls are not runs.
	for _, argv := range [][]string{
		{"agy"}, {"agy", "-i", "x"}, {"agy", "models"}, {"agy", "mcp", "list"},
		{"agy", "--", "-p"}, {"agy", "-p", "x", "--output-format", "json"}, {"agy", "-p", "x", "--output-format=stream-json"},
	} {
		if r, err := Classify(argv); err == nil {
			t.Errorf("%v: classified as %q, want a refusal", argv, r.Kind)
		}
	}
	if r, _ := Classify([]string{"agy", "-p", "find the bugs in this diff"}); r.DerivedLabel() != "agy task: find the bugs in this diff" {
		t.Errorf("label %q", r.DerivedLabel())
	}
	if _, err := ClassifyContainer([]string{"agy", "-p", "x"}); err == nil || !strings.Contains(err.Error(), "drop --container") {
		t.Errorf("container: %v", err)
	}
}

// agy's stdout is the answer file and its stderr the log, in a ledger row of
// its own harness.
func TestRun_AntigravityPrint(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_ANSWER", "two blockers found")
	res := p.run(p.options("agy", "--mode", "plan", "-p", "review the diff"))
	if res.Exit != 0 {
		t.Fatalf("exit %d", res.Exit)
	}
	if last := p.file(res.RunID, runstore.FileLastMD); last != "two blockers found\n" {
		t.Fatalf("last.md %q", last)
	}
	raw := p.file(res.RunID, runstore.FileRawLog)
	if !strings.Contains(raw, "thinking about") || strings.Contains(raw, "blockers") {
		t.Fatalf("raw.log %q: want the progress only", raw)
	}
	if !strings.Contains(p.stdout.String(), "two blockers found") {
		t.Fatalf("report %q", p.stdout.String())
	}
	if r := p.row(res.RunID); r.Harness != "antigravity-print" || r.IsOpen() {
		t.Fatalf("row harness %q open %v", r.Harness, r.IsOpen())
	}
}

func TestRun_AntigravityFailureKeepsItsExit(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_EXIT", "3")
	if res := p.run(p.options("agy", "-p", "x")); res.Exit != 3 {
		t.Fatalf("exit %d, want 3", res.Exit)
	}
}
