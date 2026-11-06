package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/paths"
)

// fakeOldHomeStore lays out what older versions left in ~/.wipnote when $HOME
// was used as a project, plus the app's own files.
func fakeOldHomeStore(t *testing.T, app string) map[string]string {
	t.Helper()
	files := map[string]string{
		"features/feat-11111111.html":      "<article id=feat-11111111>old</article>",
		"sessions/s1.html":                 "s1",
		"sessions/s1/events.ndjson":        "{}",
		"claims/c.html":                    "c",
		"sessions-ledger.html":             "ledger",
		"session-families.json":            "{}",
		"archive/2026-01/x.tar.gz":         "x",
		"config.json":                      `{"user":true}`,
		"projects/-x/project.json":         "{}",
		"logs/progress/feat-11111111.md":   "log",
		"debug.log":                        "dbg",
		".launch-mode":                     "m",
		".active-session":                  "a",
		"writer.lease.pid":                 "1",
		"sessions-ledger.html.lock":        "",
		".gitignore":                       "old",
		"features/feat-11111111.html.lock": "",
		"sessions/s1/.session-pid":         "4242",
		"sessions/s2/.session-pid":         "4243",
	}
	for rel, body := range files {
		path := filepath.Join(app, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return files
}

// snapshotOldHomeStore is every path under root with its contents, for a no-change check.
func snapshotOldHomeStore(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if strings.HasPrefix(rel, "projects") {
			return nil // the destination lives here
		}
		line := rel + " " + info.Mode().String()
		if info.Mode().IsRegular() {
			data, _ := os.ReadFile(path)
			line += " " + string(data)
		}
		out = append(out, line)
		return nil
	})
	sort.Strings(out)
	return out
}

func TestStoreImportHome(t *testing.T) {
	p := newExternalProject(t)
	home := filepath.Join(p.base, "home")
	app := filepath.Join(home, ".wipnote")
	fakeOldHomeStore(t, app)
	before := snapshotOldHomeStore(t, app)
	dest := filepath.Join(paths.ShadowRootFor(home), ".wipnote")
	want := []string{"archive/2026-01/x.tar.gz", "claims/c.html", "features/feat-11111111.html",
		"session-families.json", "sessions-ledger.html", "sessions/s1.html", "sessions/s1/events.ndjson"}

	out, err := runCLI(t, "store", "import-home", "--dry-run")
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range want {
		if !strings.Contains(out, "would copy     "+rel) {
			t.Errorf("dry run does not list %s:\n%s", rel, out)
		}
	}
	if strings.Contains(out, ".session-pid") {
		t.Errorf("dry run lists a .session-pid marker:\n%s", out)
	}
	for _, rel := range []string{"config.json", "projects", "logs", "debug.log", ".launch-mode", ".active-session", ".lock", ".pid", ".gitignore"} {
		if strings.Contains(out, rel) && !strings.Contains(out, "Dry run") {
			t.Errorf("dry run lists app or runtime file %s", rel)
		}
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("dry run created the destination")
	}

	if _, err := runCLI(t, "store", "import-home"); err != nil {
		t.Fatal(err)
	}
	for _, rel := range want {
		if _, err := os.Stat(filepath.Join(dest, rel)); err != nil {
			t.Errorf("not copied: %s", rel)
		}
	}
	for _, rel := range []string{"config.json", "projects", "logs", "debug.log", ".launch-mode", ".active-session", "writer.lease.pid", "sessions-ledger.html.lock", "sessions/s1/.session-pid", "sessions/s2/.session-pid"} {
		if _, err := os.Stat(filepath.Join(dest, rel)); err == nil {
			t.Errorf("copied app or runtime file %s", rel)
		}
	}
	if got := snapshotOldHomeStore(t, app); strings.Join(got, "\n") != strings.Join(before, "\n") {
		t.Fatal("~/.wipnote changed")
	}
	root := filepath.Dir(dest)
	if out := extGit(t, root, "status", "--porcelain"); out != "" {
		t.Fatalf("store not committed:\n%s", out)
	}

	// A rerun copies nothing new and never overwrites.
	edited := filepath.Join(dest, "features", "feat-11111111.html")
	if err := os.WriteFile(edited, []byte("newer"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err = runCLI(t, "store", "import-home")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Copied 0 file(s)") {
		t.Fatalf("rerun copied something:\n%s", out)
	}
	if data, _ := os.ReadFile(edited); string(data) != "newer" {
		t.Fatal("rerun overwrote a file")
	}

	// From $HOME the old items are reachable; a folder below does not adopt them.
	t.Chdir(home)
	if got, err := findWipnoteDir(); err != nil || got != dest {
		t.Fatalf("at $HOME: %s, %v", got, err)
	}
	below := filepath.Join(home, "below")
	if err := os.MkdirAll(below, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := paths.StoreOwnerFor(below); got != "" {
		t.Fatalf("a folder below $HOME adopted its store: %s", got)
	}
}

func TestStoreImportHome_NothingToImport(t *testing.T) {
	newExternalProject(t)
	if _, err := runCLI(t, "store", "import-home"); err == nil {
		t.Fatal("import-home succeeded with no ~/.wipnote")
	}
}
