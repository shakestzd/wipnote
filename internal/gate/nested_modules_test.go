package gate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/guardprofile"
)

func writeNestedFile(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// bug-9f3b580f: the gate used to check one module. Now it runs build, vet and
// test in every go.mod folder, and skips the folders the brief names.
func TestDetectPlan_GoNestedModules_RunsEveryModule(t *testing.T) {
	root := setupGateTestProject(t)
	for _, m := range []string{"core", "plan"} {
		writeNestedFile(t, root, m+"/go.mod", "module example.com/"+m+"\n\ngo 1.24\n")
	}
	for _, skip := range []string{"vendor/x", "node_modules/x", ".claude/x", ".gotmp-exec/x", "testdata/x", ".hidden/x", "core/testdata/y"} {
		writeNestedFile(t, root, skip+"/go.mod", "module example.com/skipped\n\ngo 1.24\n")
	}
	plan, err := DetectPlan(root, root, guardprofile.PhaseQuality)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range plan.Commands {
		rel, err := filepath.Rel(root, c.Dir)
		if err != nil || c.Dir == "" {
			t.Fatalf("command %q has Dir %q", c.Name, c.Dir)
		}
		got[c.Name+"@"+filepath.ToSlash(rel)] = true
	}
	for _, want := range []string{
		"go build@.", "go vet@.", "go test@.",
		"go build (core)@core", "go vet (core)@core", "go test (core)@core",
		"go build (plan)@plan", "go vet (plan)@plan", "go test (plan)@plan",
	} {
		if !got[want] {
			t.Errorf("missing command %q in %v", want, got)
		}
	}
	if len(plan.Commands) != 9 {
		t.Errorf("got %d commands, want 9 (3 modules x 3): %v", len(plan.Commands), got)
	}
}

// One module keeps the old plan: three commands, no module folder, plain names.
func TestDetectPlan_GoSingleModule_Unchanged(t *testing.T) {
	root := setupGateTestProject(t)
	plan, err := DetectPlan(root, root, guardprofile.PhaseQuality)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range plan.Commands {
		if c.Dir != "" {
			t.Errorf("%s has Dir %q, want empty", c.Name, c.Dir)
		}
		names = append(names, c.Name)
	}
	if strings.Join(names, ",") != "go build,go vet,go test" {
		t.Errorf("names = %v", names)
	}
}

// A failing nested module fails the gate and the summary names it.
func TestRunSession_NestedModuleFailureFailsGate(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real Go gate commands")
	}
	root := setupGateTestProject(t)
	writeNestedFile(t, root, "bad/go.mod", "module example.com/bad\n\ngo 1.24\n")
	writeNestedFile(t, root, "bad/bad.go", "package bad\n\nfunc F() int { return \"x\" }\n")
	var out, errb bytes.Buffer
	res, err := RunSession(RunOptions{ProjectRoot: root, SessionID: "sess-nested", Source: "check", Phase: guardprofile.PhaseQuality, Harness: "codex", Stdout: &out, Stderr: &errb})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed {
		t.Fatal("gate passed with a broken nested module")
	}
	if !strings.Contains(res.OutputSummary, "go build (bad) failed") {
		t.Errorf("summary %q does not name the failed module", res.OutputSummary)
	}
}

// writeProfile approves a guard profile whose quality guards are cmds.
func writeProfile(t *testing.T, root string, cmds map[string]string, order []string) {
	t.Helper()
	prof := &guardprofile.Profile{Guards: map[string][]guardprofile.Guard{}}
	yaml := "guards:\n  quality:\n"
	for _, n := range order {
		prof.Guards[guardprofile.PhaseQuality] = append(prof.Guards[guardprofile.PhaseQuality], guardprofile.Guard{Name: n, Cmd: cmds[n]})
		yaml += "    - name: " + n + "\n      cmd: " + cmds[n] + "\n"
	}
	yaml += "approved:\n  signature: " + guardprofile.Signature(prof) + "\n"
	writeNestedFile(t, root, ".wipnote/guard-profile.yaml", yaml)
}

// bug-9f3b580f: a profile with root-only commands still gets the nested
// modules, named like "go build (core)". The profile commands run first.
func TestDetectPlan_Profile_AddsUnnamedNestedModules(t *testing.T) {
	root := setupGateTestProject(t)
	writeNestedFile(t, root, "core/go.mod", "module example.com/core\n\ngo 1.24\n")
	writeNestedFile(t, root, "plan/go.mod", "module example.com/plan\n\ngo 1.24\n")
	writeProfile(t, root, map[string]string{"build": "go build ./...", "test": "cd plan && go test ./..."}, []string{"build", "test"})
	plan, err := DetectPlan(root, root, guardprofile.PhaseQuality)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range plan.Commands {
		names = append(names, c.Name)
	}
	want := "build,test,go build (core),go vet (core),go test (core)"
	if strings.Join(names, ",") != want {
		t.Errorf("names = %v, want %s (plan is named by the profile, so no duplicate)", names, want)
	}
	if len(plan.GuardNames) != 2 {
		t.Errorf("GuardNames = %v, want only the 2 profile guards", plan.GuardNames)
	}
}

// Each way to name a folder counts: cd, -C, and a guard cwd.
func TestDetectPlan_Profile_NamedModulesGetNoDuplicate(t *testing.T) {
	root := setupGateTestProject(t)
	writeNestedFile(t, root, "core/go.mod", "module example.com/core\n\ngo 1.24\n")
	writeNestedFile(t, root, "plan/go.mod", "module example.com/plan\n\ngo 1.24\n")
	writeProfile(t, root, map[string]string{"a": "cd core && go test ./...", "b": "go vet -C plan ./..."}, []string{"a", "b"})
	plan, err := DetectPlan(root, root, guardprofile.PhaseQuality)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Commands) != 2 {
		t.Errorf("got %d commands, want 2: %+v", len(plan.Commands), plan.Commands)
	}
}

// A broken nested module fails a gate whose profile names only the root.
func TestRunSession_ProfileRootOnly_NestedFailureFailsGate(t *testing.T) {
	if testing.Short() {
		t.Skip("runs real Go gate commands")
	}
	root := setupGateTestProject(t)
	writeNestedFile(t, root, "bad/go.mod", "module example.com/bad\n\ngo 1.24\n")
	writeNestedFile(t, root, "bad/bad.go", "package bad\n\nfunc F() int { return \"x\" }\n")
	writeProfile(t, root, map[string]string{"build": "go build ./..."}, []string{"build"})
	var out, errb bytes.Buffer
	res, err := RunSession(RunOptions{ProjectRoot: root, SessionID: "sess-prof", Source: "check", Phase: guardprofile.PhaseQuality, Harness: "codex", Stdout: &out, Stderr: &errb})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || !strings.Contains(res.OutputSummary, "go build (bad) failed") {
		t.Errorf("passed=%v summary=%q", res.Passed, res.OutputSummary)
	}
}
