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
