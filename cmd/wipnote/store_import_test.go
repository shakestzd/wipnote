package main

import (
	"io/fs"
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

// snapshotDiff lists the snapshot lines only one side has, so a failed
// no-change check names the path that changed (bug-a1e7bb9b).
func snapshotDiff(before, after []string) string {
	in := func(lines []string) map[string]bool {
		m := map[string]bool{}
		for _, l := range lines {
			m[l] = true
		}
		return m
	}
	b, a := in(before), in(after)
	var out []string
	for _, l := range before {
		if !a[l] {
			out = append(out, "- "+l)
		}
	}
	for _, l := range after {
		if !b[l] {
			out = append(out, "+ "+l)
		}
	}
	return strings.Join(out, "\n")
}

// snapshotOldHomeStore is every file under root with its contents, for a
// no-change check. It leaves out the destination (projects/) and the app's
// runtime state, the entries store import-home itself treats as runtime
// (logs, debug.log, session markers, pid, lock, socket and temp files):
// the CLI's ordinary pre-run may write those while the test runs, which is
// not import-home changing the old store (bug-a1e7bb9b). Project data and
// the user's config files stay in the snapshot. A file that vanishes while
// the walk runs is skipped instead of cutting the snapshot short.
func snapshotOldHomeStore(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			t.Fatalf("snapshot %s: %v", path, err)
		}
		rel, _ := filepath.Rel(root, path)
		if rel != "." && snapshotSkips(rel, d) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil // folders hold no data; a runtime marker may add one
		}
		info, err := d.Info()
		if err != nil {
			return nil // removed since the directory was read
		}
		line := rel + " " + info.Mode().String()
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			line += " " + string(data)
		}
		out = append(out, line)
		return nil
	})
	sort.Strings(out)
	return out
}

// snapshotKeeps are app entries import skips that the no-change check still
// covers: the user's own settings, which nothing in the test may write.
var snapshotKeeps = map[string]bool{"config.json": true, "config.local.json": true, ".gitignore": true, "styles.css": true}

func snapshotSkips(rel string, d fs.DirEntry) bool {
	if rel == "projects" || strings.HasPrefix(rel, "projects"+string(filepath.Separator)) {
		return true // the destination lives here
	}
	return skipImportEntry(rel, d) && !snapshotKeeps[rel]
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
	if diff := snapshotDiff(before, snapshotOldHomeStore(t, app)); diff != "" {
		t.Fatalf("~/.wipnote changed:\n%s", diff)
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

// The no-change check ignores runtime writes the CLI makes on its own but
// still names a changed project file or user config (bug-a1e7bb9b).
func TestSnapshotOldHomeStore_RuntimeWritesAreNotChanges(t *testing.T) {
	app := filepath.Join(t.TempDir(), ".wipnote")
	fakeOldHomeStore(t, app)
	before := snapshotOldHomeStore(t, app)
	for rel, body := range map[string]string{
		"debug.log": "more", "logs/x.log": "l", ".active-session": "b", ".otel-notice-shown": "1",
		"writer.lease.pid": "99", "sessions-ledger.html.lock": "x", "sessions/s9/.session-pid": "1", "x.tmp": "t",
	} {
		path := filepath.Join(app, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if diff := snapshotDiff(before, snapshotOldHomeStore(t, app)); diff != "" {
		t.Fatalf("runtime writes read as changes: %q", diff)
	}
	for _, rel := range []string{"features/feat-11111111.html", "config.json"} {
		if err := os.WriteFile(filepath.Join(app, rel), []byte("changed"), 0o644); err != nil {
			t.Fatal(err)
		}
		if diff := snapshotDiff(before, snapshotOldHomeStore(t, app)); !strings.Contains(diff, rel) {
			t.Fatalf("change to %s not named: %q", rel, diff)
		}
	}
}
