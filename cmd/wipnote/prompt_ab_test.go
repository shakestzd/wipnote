package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Tests for the before-and-after prompt check (feat-217fb5f2). The check
// exists so a guidance change has proof that agent behaviour held.

const promptABPy = "../../plugin/evals/prompt-ab/prompt_ab.py"

func needPython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not installed")
	}
}

// tu builds one assistant transcript line holding one tool_use.
func tu(name string, input map[string]any) string {
	b, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "name": name, "input": input}}}})
	return string(b)
}

func bash(cmd string) string { return tu("Bash", map[string]any{"command": cmd}) }
func edit(path string) string {
	return tu("Write", map[string]any{"file_path": path, "content": "x"})
}

func runBehaviours(t *testing.T, lines ...string) map[string]bool {
	t.Helper()
	needPython(t)
	f := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", promptABPy, "behaviours", "--item", "feat-aaaa1111", f).Output()
	if err != nil {
		t.Fatalf("behaviours failed: %v", err)
	}
	got := map[string]bool{}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("bad json %q: %v", out, err)
	}
	return got
}

func TestPromptABBehavioursAllHold(t *testing.T) {
	got := runBehaviours(t,
		bash("wipnote feature start feat-aaaa1111"),
		edit("/w/x_test.go"),
		bash("go test ./..."),
		edit("/w/x.go"),
		bash("go build ./... && go vet ./... && go test ./..."),
		bash(`git commit -m "feat-aaaa1111: add x"`),
	)
	want := map[string]bool{"started": true, "test_first": true, "gate": true, "commit_names_item": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %v, want %v", k, got[k], v)
		}
	}
}

func TestPromptABBehavioursImplementationFirst(t *testing.T) {
	got := runBehaviours(t,
		edit("/w/x.go"),
		edit("/w/x_test.go"),
		bash(`git commit -m "add x"`),
	)
	for _, k := range []string{"started", "test_first", "gate", "commit_names_item"} {
		if got[k] {
			t.Errorf("%s = true, want false", k)
		}
	}
}

// Whether the test failed is not visible in the record. Order of writes is.
func TestPromptABBehavioursOrderOfWritesDecides(t *testing.T) {
	if !runBehaviours(t, edit("/w/x_test.go"), edit("/w/x.go"))["test_first"] {
		t.Error("test_first = false for a test written before the implementation")
	}
}

// Workers often batch steps into one shell command. Position in it decides.
func TestPromptABBehavioursOrderWithinOneCommand(t *testing.T) {
	if !runBehaviours(t, bash("cat > x_test.go <<'EOF'\nt\nEOF\ncat >> x.go <<'EOF'\ni\nEOF"))["test_first"] {
		t.Error("test_first = false when the test comes first inside one command")
	}
	if runBehaviours(t, bash("cat >> x.go <<'EOF'\ni\nEOF\ncat > x_test.go <<'EOF'\nt\nEOF"))["test_first"] {
		t.Error("test_first = true when the implementation comes first inside one command")
	}
}

// writeResults lays out <dir>/<side>/<task>/run<N>.json from pass counts.
func writeResults(t *testing.T, dir, side string, passes int, behaviour string) {
	t.Helper()
	for n := 1; n <= 3; n++ {
		p := filepath.Join(dir, side, "add-func")
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		rec := map[string]bool{"started": true, "test_first": true, "gate": true, "commit_names_item": true}
		rec[behaviour] = n <= passes
		b, _ := json.Marshal(rec)
		if err := os.WriteFile(filepath.Join(p, "run"+string(rune('0'+n))+".json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func runCompare(t *testing.T, oldPasses, newPasses int) (string, string, error) {
	t.Helper()
	needPython(t)
	dir := t.TempDir()
	writeResults(t, dir, "old", oldPasses, "test_first")
	writeResults(t, dir, "new", newPasses, "test_first")
	out := filepath.Join(dir, "comparison.md")
	cmd := exec.Command("python3", promptABPy, "compare", "--results", dir,
		"--old-ref", "main", "--new-ref", "planted", "--runs", "3", "--out", out)
	_, err := cmd.CombinedOutput()
	b, _ := os.ReadFile(out)
	return string(b), dir, err
}

func TestPromptABFlagsPlantedRegression(t *testing.T) {
	md, _, err := runCompare(t, 3, 0)
	if err == nil {
		t.Error("compare exited 0, want non-zero for a regression")
	}
	if !strings.Contains(md, "REGRESSION") || !strings.Contains(md, "test_first") {
		t.Errorf("comparison does not flag test_first:\n%s", md)
	}
}

func TestPromptABPassRuleBoundaries(t *testing.T) {
	cases := []struct {
		oldP, newP int
		flagged    bool
	}{
		{3, 1, true},  // 3/3 then fewer than two: regression
		{3, 2, false}, // two of three on the new ref is allowed
		{3, 3, false},
		{2, 0, false}, // the old ref did not hold in three of three
	}
	for _, c := range cases {
		md, _, err := runCompare(t, c.oldP, c.newP)
		if flagged := strings.Contains(md, "REGRESSION"); flagged != c.flagged {
			t.Errorf("old %d/3 new %d/3: flagged=%v want %v\n%s", c.oldP, c.newP, flagged, c.flagged, md)
		}
		if (err != nil) != c.flagged {
			t.Errorf("old %d/3 new %d/3: exit error=%v want flagged=%v", c.oldP, c.newP, err, c.flagged)
		}
	}
}

func TestPromptABScriptDryRunPlan(t *testing.T) {
	out, err := exec.Command("bash", "../../scripts/prompt-ab.sh", "--dry-run", "--old", "main", "--new", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("dry run failed: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{"worktree old: main", "worktree new: HEAD", "build: ", "runs per task per ref: 3", "task: add-func", "comparison file: "} {
		if !strings.Contains(s, want) {
			t.Errorf("dry-run plan lacks %q:\n%s", want, s)
		}
	}
	if n := strings.Count(s, "task: "); n < 3 || n > 5 {
		t.Errorf("task count %d, want 3 to 5", n)
	}
}

func TestPromptABDocStatesPassRule(t *testing.T) {
	b, err := os.ReadFile("../../docs/design/prompt-ab.md")
	if err != nil {
		t.Fatal(err)
	}
	want := "a behaviour that holds in three of three runs on the old ref and in fewer than two on the new ref is a regression"
	if !strings.Contains(strings.ToLower(string(b)), want) {
		t.Errorf("docs/design/prompt-ab.md lacks the pass rule %q", want)
	}
}

// A worker may write code through a shell redirect. The check must see it.
func TestPromptABBehavioursShellWrittenImplementation(t *testing.T) {
	got := runBehaviours(t,
		edit("/w/x_test.go"),
		bash("go test ./... 2>&1 | head"),
		bash("cat >> x.go <<'EOF'\nfunc X() {}\nEOF"),
	)
	if !got["test_first"] {
		t.Error("test_first = false for a test run before a shell-written implementation")
	}
	got = runBehaviours(t,
		bash("cat >> x.go <<'EOF'\nfunc X() {}\nEOF"),
		edit("/w/x_test.go"),
		bash("go test ./..."),
	)
	if got["test_first"] {
		t.Error("test_first = true for an implementation written by shell first")
	}
}

// `wipnote check --gate` is the project's own gate and counts as a gate run.
func TestPromptABBehavioursWipnoteCheckGate(t *testing.T) {
	if !runBehaviours(t, bash("wipnote check --gate"))["gate"] {
		t.Error("gate = false after wipnote check --gate")
	}
	if runBehaviours(t, bash("go test ./..."))["gate"] {
		t.Error("gate = true for go test alone")
	}
}
