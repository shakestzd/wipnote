package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/procsweep"
	"github.com/shakestzd/wipnote/internal/registry"
	"github.com/shakestzd/wipnote/internal/statusall"
)

// statusAllFixture makes two temp projects, one readable with an in-progress
// feature and one whose folder is gone, registered in a temp registry.
func statusAllFixture(t *testing.T) (good, regPath string) {
	t.Helper()
	good = makeProjectWithItems(t, 0, 0, 0)
	writeStatusAllItem(t, good, "feat-5a5a0001", "Show every project")
	gone := filepath.Join(t.TempDir(), "gone")
	regPath = withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: good, Name: "good"},
		{ProjectDir: gone, Name: "gone"},
	})
	return good, regPath
}

func writeStatusAllItem(t *testing.T, project, id, title string) {
	t.Helper()
	dir := filepath.Join(project, ".wipnote", "features")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	html := `<!DOCTYPE html><html><body><article id="` + id + `" data-type="feature" data-status="in-progress" ` +
		`data-priority="medium"><header><h1>` + title + `</h1></header></article></body></html>`
	if err := os.WriteFile(filepath.Join(dir, id+".html"), []byte(html), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runStatusAllCmd runs `wipnote status <args>` through the real prerun.
func runStatusAllCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := buildRootCmdForTest(t)
	root.AddCommand(statusCmd())
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"status"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func statusAllTree(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil {
			b.WriteString(path + " " + info.ModTime().String() + "\n")
		}
		return nil
	})
	return b.String()
}

func TestStatusAll_TextListsEveryProjectAndWritesNothing(t *testing.T) {
	good, regPath := statusAllFixture(t)
	isolateProjectDir(t, good)
	regBefore, _ := os.ReadFile(regPath)
	treeBefore := statusAllTree(t, filepath.Join(good, ".wipnote"))

	out, err := runStatusAllCmd(t, "--all", "--no-gh", "--no-procs", "--project-dir", good)
	if err != nil {
		t.Fatalf("status --all: %v\n%s", err, out)
	}
	for _, want := range []string{"good", "feat-5a5a0001 Show every project", "gone  error: cannot read project", "gh: skipped"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output lacks %q:\n%s", want, out)
		}
	}
	if regAfter, _ := os.ReadFile(regPath); !bytes.Equal(regBefore, regAfter) {
		t.Fatalf("status --all rewrote the registry:\nbefore %s\nafter  %s", regBefore, regAfter)
	}
	if treeAfter := statusAllTree(t, filepath.Join(good, ".wipnote")); treeAfter != treeBefore {
		t.Fatalf("status --all wrote into .wipnote/:\nbefore\n%s\nafter\n%s", treeBefore, treeAfter)
	}
}

func TestStatusAll_JSONMatchesTheSchema(t *testing.T) {
	good, _ := statusAllFixture(t)
	isolateProjectDir(t, good)

	out, err := runStatusAllCmd(t, "--all", "--json", "--no-gh", "--no-procs")
	if err != nil {
		t.Fatalf("status --all --json: %v\n%s", err, out)
	}
	var r statusall.Report
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if r.Schema != statusall.Schema || len(r.Projects) != 2 {
		t.Fatalf("report = %+v", r)
	}
	for _, p := range r.Projects {
		if p.Name == "good" && (p.Sources["gh"] != statusall.SourceSkipped || !strings.HasPrefix(p.Sources["herdr"], statusall.SourceNotAvailable)) {
			t.Fatalf("good sources = %v", p.Sources)
		}
	}
}

func TestStatusAll_JSONNeedsAll(t *testing.T) {
	good, _ := statusAllFixture(t)
	isolateProjectDir(t, good)
	if _, err := runStatusAllCmd(t, "--json"); err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("status --json without --all: err = %v", err)
	}
}

// Without --all, status keeps its per-project behaviour.
func TestStatus_WithoutAllIsUnchanged(t *testing.T) {
	good, _ := statusAllFixture(t)
	isolateProjectDir(t, good)
	out, err := runStatusAllCmd(t, "--project-dir", good)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	_ = out // runStatus prints to the process stdout; it must simply not fail.
}

func TestStatusAll_NoProcsSkipsTheSweep(t *testing.T) {
	good, _ := statusAllFixture(t)
	isolateProjectDir(t, good)
	calls := 0
	orig := statusAllProcLister
	statusAllProcLister = procsweep.ListerFunc(func(context.Context) ([]procsweep.RawProc, error) {
		calls++
		return nil, nil
	})
	t.Cleanup(func() { statusAllProcLister = orig })

	out, err := runStatusAllCmd(t, "--all", "--no-gh", "--no-procs")
	if err != nil || !strings.Contains(out, "procs: skipped") || calls != 0 {
		t.Fatalf("--no-procs: err %v calls %d\n%s", err, calls, out)
	}
	out, err = runStatusAllCmd(t, "--all", "--no-gh")
	if err != nil || strings.Contains(out, "procs: skipped") || calls != 1 {
		t.Fatalf("default: err %v calls %d\n%s", err, calls, out)
	}
}
