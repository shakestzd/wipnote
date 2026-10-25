package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskCompleted_NeverBlocksOnFailingTests pins the owner decision
// (feat-726637b8): hooks never block, so TaskCompleted returns Continue even
// in a project whose tests fail and whose config still carries the retired
// block flag. `wipnote check --gate` covers quality on demand.
func TestTaskCompleted_NeverBlocksOnFailingTests(t *testing.T) {
	const sid = "aaaa3333-bbbb-cccc-dddd-eeee00006666"
	projectDir := taskClaimProject(t, sid, true)
	writeFile(t, filepath.Join(projectDir, "go.mod"), "module broken\n")
	writeFile(t, filepath.Join(projectDir, "x_test.go"), "package broken\nfunc TestX(t *T) {}\n")
	writeFile(t, filepath.Join(projectDir, ".wipnote", "config.json"),
		`{"block_task_completion_on_quality_failure":true}`)
	stubTaskStepFns(t)
	ev := &CloudEvent{SessionID: sid, CWD: projectDir, TaskID: "t-gate", TaskSubject: "Finish feat-abcd1234"}
	res, err := TaskCompleted(ev)
	if err != nil || res == nil || !res.Continue {
		t.Fatalf("TaskCompleted = (%+v, %v), want Continue and no error", res, err)
	}
}

// TestHooksSource_HasNoTaskQualityGate is the other side: the gate and its
// config key are gone from the hooks package, not just switched off.
func TestHooksSource_HasNoTaskQualityGate(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	banned := []string{"block_task_completion_on_quality_failure", "runTaskCompletionGate", "taskCompletionGateEnabled"}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, b := range banned {
			if strings.Contains(string(src), b) {
				t.Errorf("%s still mentions %s", file, b)
			}
		}
	}
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
