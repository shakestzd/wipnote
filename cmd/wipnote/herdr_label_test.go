//go:build !integration

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

const labelOKScript = `{"rules": [
  {"match": ["pane", "rename", "*", "**"], "replies": [{"shape": {"as": "ok"}}]}
]}`

const labelSlowScript = `{"rules": [
  {"match": ["pane", "rename", "*", "**"], "replies": [{"delay_ms": 5000, "shape": {"as": "ok"}}]}
]}`

// installLabelFake links this test binary into a temp dir as "herdr" (TestMain
// replays the script through herdr.FakeMain), puts it first on PATH and
// returns the fake's call log. It does not set HERDR_ENV: each test decides.
func installLabelFake(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, filepath.Join(dir, "herdr")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "script.json")
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(herdr.FakeScriptEnv, path)
	return path + ".log.ndjson"
}

func herdrCalls(t *testing.T, log string) []string {
	t.Helper()
	calls, err := herdr.ReadFakeCalls(log)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, c := range calls {
		out = append(out, strings.Join(c.Argv, " "))
	}
	return out
}

func inHerdrPane(t *testing.T, pane string) {
	t.Helper()
	t.Setenv("HERDR_ENV", "1")
	t.Setenv("HERDR_PANE_ID", pane)
}

// labelTestFeature makes a temp project with one feature and returns the
// project root and the feature id.
func labelTestFeature(t *testing.T, title string) (string, string) {
	t.Helper()
	tmpDir, hgDir := setupResetTest(t)
	isolateProjectDir(t, tmpDir)
	projectDirFlag = tmpDir
	t.Cleanup(func() { projectDirFlag = "" })
	trackID := testSetupTrack(t, hgDir)
	if err := testCreate("feature", title, trackID, "medium", false, false); err != nil {
		t.Fatalf("create feature: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(hgDir, "features", "feat-*.html"))
	if len(files) != 1 {
		t.Fatalf("expected 1 feature file, got %d", len(files))
	}
	node, err := htmlparse.ParseFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	return tmpDir, node.ID
}

func TestFeatureStart_LabelsHerdrPane(t *testing.T) {
	_, id := labelTestFeature(t, "Label the pane")
	log := installLabelFake(t, labelOKScript)
	inHerdrPane(t, "w1:p4")
	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start: %v", err)
	}
	want := "pane rename w1:p4 " + id + ": Label the pane"
	if got := herdrCalls(t, log); len(got) != 1 || got[0] != want {
		t.Fatalf("herdr calls = %q, want [%q]", got, want)
	}
}

func TestFeatureStart_NoHerdrCallOutsideHerdr(t *testing.T) {
	_, id := labelTestFeature(t, "Label the pane")
	log := installLabelFake(t, labelOKScript)
	t.Setenv("HERDR_PANE_ID", "w1:p4") // stale id outside herdr names nothing
	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := herdrCalls(t, log); len(got) != 0 {
		t.Fatalf("herdr ran outside herdr: %q", got)
	}
}

func TestFeatureStart_SubagentLeavesPaneLabel(t *testing.T) {
	_, id := labelTestFeature(t, "Label the pane")
	log := installLabelFake(t, labelOKScript)
	inHerdrPane(t, "w1:p4")
	if err := wiSetStatusWithAgent("feature", id, "in-progress", "sess-1", "sub-agent-1"); err != nil {
		t.Fatalf("start: %v", err)
	}
	if got := herdrCalls(t, log); len(got) != 0 {
		t.Fatalf("a subagent's start relabelled the pane: %q", got)
	}
}

func TestFeatureStart_SlowHerdrDoesNotFailOrHang(t *testing.T) {
	_, id := labelTestFeature(t, "Label the pane")
	log := installLabelFake(t, labelSlowScript)
	inHerdrPane(t, "w1:p4")
	start := time.Now()
	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start failed on a slow herdr: %v", err)
	}
	if got := herdrCalls(t, log); len(got) != 1 {
		t.Fatalf("herdr calls = %q, want the one rename", got)
	}
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("start took %s with a hung herdr; the label call must be cut at %s", d, herdr.LabelTimeout)
	}
}

func TestLabelHerdrPaneForWorkItem(t *testing.T) {
	title := "A very long feature title that cannot fit in a herdr pane label"
	root, id := labelTestFeature(t, title)

	t.Run("inside herdr", func(t *testing.T) {
		log := installLabelFake(t, labelOKScript)
		inHerdrPane(t, "w1:p9")
		labelHerdrPaneForWorkItem(root, id)
		want := "pane rename w1:p9 " + herdr.WorkLabel(id, title)
		got := herdrCalls(t, log)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("herdr calls = %q, want [%q]", got, want)
		}
		if !strings.HasSuffix(got[0], "…") {
			t.Fatalf("long title not cut: %q", got[0])
		}
	})
	t.Run("outside herdr", func(t *testing.T) {
		log := installLabelFake(t, labelOKScript)
		labelHerdrPaneForWorkItem(root, id)
		if got := herdrCalls(t, log); len(got) != 0 {
			t.Fatalf("herdr ran outside herdr: %q", got)
		}
	})
	t.Run("no work item", func(t *testing.T) {
		log := installLabelFake(t, labelOKScript)
		inHerdrPane(t, "w1:p9")
		labelHerdrPaneForWorkItem(root, "")
		if got := herdrCalls(t, log); len(got) != 0 {
			t.Fatalf("herdr ran with no work item: %q", got)
		}
	})
	t.Run("herdr missing", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		inHerdrPane(t, "w1:p9")
		labelHerdrPaneForWorkItem(root, id) // must return quietly
	})
}

func TestLaunchHerdrLabel_DoesNotBlockLaunch(t *testing.T) {
	root, id := labelTestFeature(t, "Label the pane")
	log := installLabelFake(t, labelSlowScript)
	inHerdrPane(t, "w1:p2")
	start := time.Now()
	done := launchHerdrLabel(root, id)
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("launchHerdrLabel held the launch for %s", d)
	}
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatalf("label call outlived its %s timeout", herdr.LabelTimeout)
	}
	if got := herdrCalls(t, log); len(got) != 1 || !strings.HasPrefix(got[0], "pane rename w1:p2 "+id+": ") {
		t.Fatalf("herdr calls = %q", got)
	}
}
