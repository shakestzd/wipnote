package paths

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type layeredCfg struct {
	A    string `json:"a"`
	B    string `json:"b"`
	C    string `json:"c"`
	Dir  string `json:"dir"`
	Keep int    `json:"keep"`
}

// layeredProject is a temp project and a temp HOME with the given user,
// project and local files ("" leaves a file out).
func layeredProject(t *testing.T, user, project, local string) string {
	t.Helper()
	home, dir := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	for path, body := range map[string]string{
		filepath.Join(home, ".wipnote", "config.json"): user,
		ProjectConfigPath(dir):                         project,
		LocalConfigPath(dir):                           local,
	} {
		if body == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func readLayered(t *testing.T, dir string) (layeredCfg, string, bool) {
	t.Helper()
	var got layeredCfg
	var warn bytes.Buffer
	ok := ReadLayeredConfig(dir, LayeredOptions{Section: "s", PathKeys: []string{"dir"}, Warn: &warn}, &got)
	return got, warn.String(), ok
}

// Each layer overrides the one below it, key by key, within the section:
// user < project < local. A key only a lower layer sets survives.
func TestReadLayeredConfig_LayersOverridePerKey(t *testing.T) {
	dir := layeredProject(t,
		`{"s": {"a": "user", "b": "user", "c": "user", "keep": 1}, "other": {"a": "x"}}`,
		`{"s": {"b": "project", "c": "project"}}`,
		`{"s": {"c": "local"}}`)
	got, warn, ok := readLayered(t, dir)
	want := layeredCfg{A: "user", B: "project", C: "local", Keep: 1}
	if !ok || got != want || warn != "" {
		t.Fatalf("got %+v ok %v warn %q, want %+v", got, ok, warn, want)
	}
	// without the local file the project value shows; without both, the user's
	must(t, os.Remove(LocalConfigPath(dir)))
	if got, _, _ := readLayered(t, dir); got.C != "project" {
		t.Fatalf("no local layer: c %q, want project", got.C)
	}
	must(t, os.Remove(ProjectConfigPath(dir)))
	if got, _, _ := readLayered(t, dir); got.B != "user" || got.C != "user" {
		t.Fatalf("user layer only: %+v", got)
	}
}

// A path key in the committed project file is ignored with one warning line
// naming the key and the two files it belongs in; from the user or local
// file it is used.
func TestReadLayeredConfig_PathKeysOnlyFromUserAndLocal(t *testing.T) {
	dir := layeredProject(t, "", `{"s": {"dir": "/from/project", "a": "p"}}`, "")
	got, warn, _ := readLayered(t, dir)
	if got.Dir != "" || got.A != "p" {
		t.Fatalf("project path key used: %+v", got)
	}
	if strings.Count(warn, "\n") != 1 || !strings.Contains(warn, `"s.dir"`) ||
		!strings.Contains(warn, "~/.wipnote/config.json") || !strings.Contains(warn, ".wipnote/config.local.json") {
		t.Fatalf("warning %q", warn)
	}
	dir = layeredProject(t, `{"s": {"dir": "/from/user"}}`, `{"s": {"dir": "/from/project"}}`, "")
	if got, _, _ := readLayered(t, dir); got.Dir != "/from/user" {
		t.Fatalf("user path key: %q", got.Dir)
	}
	dir = layeredProject(t, `{"s": {"dir": "/from/user"}}`, "", `{"s": {"dir": "/from/local"}}`)
	if got, warn, _ := readLayered(t, dir); got.Dir != "/from/local" || warn != "" {
		t.Fatalf("local path key: %q warn %q", got.Dir, warn)
	}
}

// A missing or malformed layer is skipped; the others still apply. No layer
// at all reports false.
func TestReadLayeredConfig_BadOrMissingLayers(t *testing.T) {
	dir := layeredProject(t, `{"s": {"a": "user"}}`, `{"s": `, `{"s": {"b": "local"}}`)
	got, _, ok := readLayered(t, dir)
	if !ok || got.A != "user" || got.B != "local" {
		t.Fatalf("got %+v ok %v", got, ok)
	}
	if _, _, ok := readLayered(t, layeredProject(t, "", "", "")); ok {
		t.Fatal("no files reported true")
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
