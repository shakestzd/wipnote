package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

// toolSeq numbers the tool_use ids the helpers hand out.
var toolSeq int

// tuResult builds an assistant tool_use line and the user tool_result line that
// answers it. The result carries the error flag and body the test gives.
func tuResult(name string, input map[string]any, isErr bool, body string) string {
	toolSeq++
	id := "toolu_" + strconv.Itoa(toolSeq)
	use, _ := json.Marshal(map[string]any{"type": "assistant", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_use", "id": id, "name": name, "input": input}}}})
	res, _ := json.Marshal(map[string]any{"type": "user", "message": map[string]any{
		"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": body, "is_error": isErr}}}})
	return string(use) + "\n" + string(res)
}

// tu builds a tool call whose result is a success.
func tu(name string, input map[string]any) string { return tuResult(name, input, false, "ok") }

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

// A command that failed does not count as a held behaviour, whatever it tried.
func TestPromptABBehavioursReadResults(t *testing.T) {
	failed := func(cmd string) string {
		return tuResult("Bash", map[string]any{"command": cmd}, true, "Exit code 1")
	}
	gate := func(lines ...string) bool { return runBehaviours(t, lines...)["gate"] }
	if gate(bash("go build ./..."), bash("go vet ./..."), failed("go test ./...")) {
		t.Error("gate = true although go test returned an error result")
	}
	if !gate(bash("go build ./..."), bash("go vet ./..."), bash("go test ./...")) {
		t.Error("gate = false for three successful gate commands")
	}
	if runBehaviours(t, failed("wipnote feature start feat-aaaa1111"))["started"] {
		t.Error("started = true for a start command that failed")
	}
	if runBehaviours(t, failed(`git commit -m "feat-aaaa1111: x"`))["commit_names_item"] {
		t.Error("commit_names_item = true for a commit that failed")
	}
	if runBehaviours(t, tuResult("Write", map[string]any{"file_path": "/w/x_test.go"}, true, "denied"), edit("/w/x.go"))["test_first"] {
		t.Error("test_first = true although the test file write failed")
	}
}

// The stream records the exit status in the result body for a failed shell command.
func TestPromptABBehavioursExitStatusInBody(t *testing.T) {
	bad := tuResult("Bash", map[string]any{"command": "go test ./..."}, false, "Exit code 2\nFAIL")
	got := runBehaviours(t, bash("go build ./..."), bash("go vet ./..."), bad)
	if got["gate"] {
		t.Error("gate = true although the result body records exit code 2")
	}
}

// A command that masks a failure is not a gate run.
func TestPromptABBehavioursMaskedFailure(t *testing.T) {
	for _, mask := range []string{"go test ./... || true", "go test ./... || :", "go test ./... ||true"} {
		got := runBehaviours(t, bash("go build ./... && go vet ./..."), bash(mask))
		if got["gate"] {
			t.Errorf("gate = true for %q", mask)
		}
	}
}

// A tool call with no result in the log did not finish, so it does not count.
func TestPromptABBehavioursMissingResult(t *testing.T) {
	use := tu("Bash", map[string]any{"command": "wipnote feature start feat-aaaa1111"})
	if runBehaviours(t, strings.SplitN(use, "\n", 2)[0])["started"] {
		t.Error("started = true for a tool call with no result")
	}
}

// runCompareDirs runs compare over a prepared results folder.
func runCompareDirs(t *testing.T, dir string) (string, int) {
	t.Helper()
	needPython(t)
	cmd := exec.Command("python3", promptABPy, "compare", "--results", dir,
		"--old-ref", "main", "--new-ref", "x", "--runs", "3", "--out", filepath.Join(dir, "c.md"))
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return string(out), code
}

// With one old run missing, 2 of 3 against 0 of 3 must not pass as "no regression".
func TestPromptABCompareRequiresRunCount(t *testing.T) {
	dir := t.TempDir()
	writeResults(t, dir, "old", 3, "test_first")
	writeResults(t, dir, "new", 0, "test_first")
	if err := os.Remove(filepath.Join(dir, "old", "add-func", "run3.json")); err != nil {
		t.Fatal(err)
	}
	out, code := runCompareDirs(t, dir)
	if code != 2 {
		t.Errorf("exit = %d, want 2\n%s", code, out)
	}
	for _, want := range []string{"add-func", "old", "2 of 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	dir = t.TempDir()
	writeResults(t, dir, "old", 3, "test_first")
	writeResults(t, dir, "new", 3, "test_first")
	if err := os.WriteFile(filepath.Join(dir, "new", "add-func", "run4.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code = runCompareDirs(t, dir); code != 2 {
		t.Errorf("exit with four new results = %d, want 2", code)
	}
}
