package oneshot

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestPlanClaude_FlagsAndCaps(t *testing.T) {
	p, why := planClaude([]string{"claude", "-p", "--model", "haiku", "--allowedTools", "Read", "Edit", "do the thing",
		"--max-turns", "3", "--max-budget-usd=0.5", "--output-format", "json", "--verbose"})
	if why != "" || p.Prompt != "do the thing" || p.MaxTurns != 3 || p.MaxBudget != 0.5 {
		t.Fatalf("plan %+v why %q", p, why)
	}
	want := []string{"--model", "haiku", "--allowedTools", "Read", "Edit", "--verbose"}
	if !slices.Equal(p.Args, want) || !slices.Equal(p.Dropped, []string{"--output-format"}) {
		t.Fatalf("args %q dropped %q", p.Args, p.Dropped)
	}
}

func TestPlanClaude_KeepsPrintPath(t *testing.T) {
	for _, argv := range [][]string{
		{"claude", "-p", "--resume", "abc", "go on"},
		{"claude", "-p", "-c", "go on"},
		{"claude", "-p"},                          // the prompt would come on stdin
		{"claude", "-p", "--unknown", "x", "y z"}, // two prompt words
		{"claude", "-p", "--max-turns", "many", "go"},
	} {
		if _, why := planClaude(argv); why == "" {
			t.Fatalf("%q planned as a worker", argv)
		}
	}
}

func TestClaudeRun_NoWorkItemKeepsPrintPath(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	w.o.WorkItem = ""
	_, ok, err := runClaudeWorker(context.Background(), w.o, Recipe{Kind: KindClaude, Argv: w.o.Argv})
	if ok || err != nil || !strings.Contains(w.p.stderr.String(), "no work item") || len(w.f.Calls()) != 0 {
		t.Fatalf("ok %v err %v stderr %s calls %v", ok, err, w.p.stderr.String(), w.f.Calls())
	}
	// With a work item it is a worker (the opposite case is every other test).
}
