package main

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// goFileLines builds a Go file of exactly n lines with distinct statements.
func goFileLines(n int) string {
	var sb strings.Builder
	sb.WriteString("package p\n")
	for i := 1; i < n; i++ {
		fmt.Fprintf(&sb, "var v%d = %d\n", i, i)
	}
	return sb.String()
}

// goFuncLines builds a Go function of exactly n lines (declaration and brace included).
func goFuncLines(name string, n int) string {
	var sb strings.Builder
	sb.WriteString("func " + name + "() {\n")
	for i := 0; i < n-2; i++ {
		fmt.Fprintf(&sb, "\t_ = %d\n", i)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func findingKeys(fs []sizeFinding) []string {
	var keys []string
	for _, f := range fs {
		keys = append(keys, f.Path+"|"+f.Name)
	}
	return keys
}

// TestChangedSizeGate_BlocksOnlyNewUnreasonedTriggers pins GitHub #312: the
// gate applies the size triggers to changed code only. Code over a trigger
// that this change introduced blocks unless a one-line reason is recorded;
// legacy code that was already over the trigger at base never blocks.
func TestChangedSizeGate_BlocksOnlyNewUnreasonedTriggers(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{
		"legacy.go":    goFileLines(600),
		"small.go":     "package p\n",
		"untouched.go": goFileLines(700),
	})
	writeRepoFile(t, dir, "legacy.go", goFileLines(605))
	writeRepoFile(t, dir, "big.go", goFileLines(hooks.ModuleLimitLines+20))
	writeRepoFile(t, dir, "small.go", "package p\n\n"+goFuncLines("Long", 60)+
		"\n// Keep is one table.\n// "+hooks.SizeOKMarker+": one lookup table, splitting hides it\n"+goFuncLines("Keep", 60)+
		"\n// "+hooks.SizeOKMarker+":\n"+goFuncLines("NoReason", 60))

	report, err := scanChangedSize(dir, resolveSizeBase(dir, "main"))
	if err != nil {
		t.Fatalf("scanChangedSize: %v", err)
	}
	got := strings.Join(findingKeys(report.blocking()), ",")
	want := "big.go|,small.go|Long,small.go|NoReason"
	if got != want {
		t.Fatalf("blocking = %q, want %q", got, want)
	}
	all := strings.Join(findingKeys(report.Findings), ",")
	if strings.Contains(all, "untouched.go") {
		t.Errorf("unchanged legacy file must not be scanned: %q", all)
	}
	var out bytes.Buffer
	writeSizeReport(&out, report)
	for _, want := range []string{"legacy.go", "already over", "one lookup table", hooks.SizeOKMarker} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}

func TestChangedSizeGate_FileMarkerRecordsReason(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{"a.go": "package p\n"})
	body := "// " + hooks.SizeOKMarker + ": generated-style table kept in one file\n" + goFileLines(hooks.ModuleLimitLines+5)
	writeRepoFile(t, dir, "big.go", body)

	var out bytes.Buffer
	if err := checkChangedCodeSize(&out, dir); err != nil {
		t.Fatalf("a recorded reason must satisfy the gate: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "generated-style table") {
		t.Errorf("report should show the recorded reason:\n%s", out.String())
	}
}

func TestChangedSizeGate_FailsWithoutReason(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{"a.go": "package p\n"})
	writeRepoFile(t, dir, "big.go", goFileLines(hooks.ModuleLimitLines+5))

	var out bytes.Buffer
	err := checkChangedCodeSize(&out, dir)
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("expected a size gate error, got %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "big.go") {
		t.Errorf("report should name big.go:\n%s", out.String())
	}
}

func TestChangedSizeGate_NotAGitRepoIsSkipped(t *testing.T) {
	var out bytes.Buffer
	if err := checkChangedCodeSize(&out, t.TempDir()); err != nil {
		t.Fatalf("no git repo should skip the size gate, got %v", err)
	}
}

// TestHealthLimitsDeriveFromHookTriggers guards the DRY fix: health used to
// hold its own copy of the fail thresholds, which could disagree with the hook.
func TestHealthLimitsDeriveFromHookTriggers(t *testing.T) {
	if moduleLimitFail != hooks.ModuleLimitLines || funcLimitFail != hooks.FuncLimitLines {
		t.Fatalf("health limits %d/%d differ from hook triggers %d/%d",
			moduleLimitFail, funcLimitFail, hooks.ModuleLimitLines, hooks.FuncLimitLines)
	}
}

func TestGoFuncSpans_KeyIncludesReceiver(t *testing.T) {
	src := "package p\n\nfunc (a *A) String() string {\n\treturn \"a\"\n}\n\nfunc (b B) String() string {\n\treturn \"b\"\n}\n"
	spans := goFuncSpans(strings.Split(src, "\n"))
	if len(spans) != 2 || spans[0].Key == spans[1].Key {
		t.Fatalf("methods on different receivers need distinct keys: %+v", spans)
	}
	if spans[0].Name != "String" || spans[0].Line != 3 || spans[0].Lines != 3 {
		t.Errorf("span = %+v, want String at line 3, 3 lines", spans[0])
	}
}

// TestCheckCmd_GateFailsOnUnreasonedSizeTrigger wires the size gate into
// `wipnote check --gate`: a new file over the module trigger with no recorded
// reason stops the gate before any build runs.
func TestCheckCmd_GateFailsOnUnreasonedSizeTrigger(t *testing.T) {
	projectRoot := setupGateTestProject(t)
	setupTempGitRepo(t, projectRoot, map[string]string{"main.go": "package main\n\nfunc main() {}\n"})
	writeRepoFile(t, projectRoot, "big.go", goFileLines(hooks.ModuleLimitLines+1))
	projectDirFlag = projectRoot
	t.Cleanup(func() { projectDirFlag = "" })

	cmd := checkCmd()
	cmd.SetArgs([]string{"--gate"})
	var execErr error
	out := captureStdout(t, func() { execErr = cmd.Execute() })
	if execErr == nil || !strings.Contains(execErr.Error(), "size gate") {
		t.Fatalf("expected a size gate failure, got %v\n%s", execErr, out)
	}
	if !strings.Contains(out, "big.go") {
		t.Errorf("gate output should name big.go:\n%s", out)
	}
}
