package oneshot

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// settingsFiles writes the user (~/.wipnote/config.json under a temp HOME),
// project and local delegate settings of a temp project; "" leaves one out.
func settingsFiles(t *testing.T, user, project, local string) string {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for path, body := range map[string]string{
		filepath.Join(home, ".wipnote", "config.json"):      user,
		filepath.Join(dir, ".wipnote", "config.json"):       project,
		filepath.Join(dir, ".wipnote", "config.local.json"): local,
	} {
		if body != "" {
			must(t, os.MkdirAll(filepath.Dir(path), 0o755))
			must(t, os.WriteFile(path, []byte(body), 0o600))
		}
	}
	return dir
}

// user < project < local, per key of the "delegate" object.
func TestSettings_Layers(t *testing.T) {
	dir := settingsFiles(t,
		`{"delegate": {"maxPanes": 2, "tailLines": 7, "keepDays": 3}}`,
		`{"delegate": {"maxPanes": 4, "tailLines": 8}}`,
		`{"delegate": {"tailLines": 9}}`)
	s := LoadSettings(dir, io.Discard)
	if s.KeepDays != 3 || s.MaxPanes != 4 || s.TailLines != 9 {
		t.Fatalf("keepDays %d (user), maxPanes %d (project), tailLines %d (local)", s.KeepDays, s.MaxPanes, s.TailLines)
	}
}

// ccxRunsDir is a host path: from the project file it is ignored with a
// warning; from the user or local file it is used; CCX_RUNS_DIR beats all.
func TestSettings_CcxRunsDirLayers(t *testing.T) {
	var warn bytes.Buffer
	dir := settingsFiles(t, "", `{"delegate": {"ccxRunsDir": "/p/runs", "maxPanes": 3}}`, "")
	if s := LoadSettings(dir, &warn); s.CcxRunsDir != "" || s.MaxPanes != 3 {
		t.Fatalf("project ccxRunsDir used: %+v", s)
	}
	if w := warn.String(); strings.Count(w, "\n") != 1 || !strings.Contains(w, "delegate.ccxRunsDir") ||
		!strings.Contains(w, "~/.wipnote/config.json") || !strings.Contains(w, ".wipnote/config.local.json") {
		t.Fatalf("warning %q", w)
	}
	dir = settingsFiles(t, `{"delegate": {"ccxRunsDir": "/u/runs"}}`, "", "")
	if s := LoadSettings(dir, io.Discard); s.CcxRunsDir != "/u/runs" {
		t.Fatalf("user ccxRunsDir %q", s.CcxRunsDir)
	}
	dir = settingsFiles(t, `{"delegate": {"ccxRunsDir": "/u/runs"}}`, "", `{"delegate": {"ccxRunsDir": " /l/runs "}}`)
	if s := LoadSettings(dir, io.Discard); s.CcxRunsDir != "/l/runs" {
		t.Fatalf("local ccxRunsDir %q", s.CcxRunsDir)
	}
	t.Setenv("CCX_RUNS_DIR", "/env/runs")
	if s := LoadSettings(dir, io.Discard); s.CcxRunsDir != "/env/runs" {
		t.Fatalf("env must win: %q", s.CcxRunsDir)
	}
}

// The local and user files are read whether or not .wipnote/ is committed:
// here the project's own .gitignore ignores the whole folder.
func TestSettings_WhenWipnoteDirIsGitignored(t *testing.T) {
	dir := settingsFiles(t, `{"delegate": {"keepDays": 4}}`, "", `{"delegate": {"ccxRunsDir": "/l/runs"}}`)
	must(t, exec.Command("git", "init", "-q", dir).Run())
	must(t, os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(".wipnote/\n"), 0o644))
	if out, err := exec.Command("git", "-C", dir, "check-ignore", ".wipnote/config.local.json").Output(); err != nil || len(out) == 0 {
		t.Fatalf("fixture: .wipnote/ not ignored: %v", err)
	}
	if s := LoadSettings(dir, io.Discard); s.KeepDays != 4 || s.CcxRunsDir != "/l/runs" {
		t.Fatalf("layers lost when .wipnote/ is ignored: %+v", s)
	}
}
