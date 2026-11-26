package scratch

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const testSession = "sess-0001"

// env is a temp store and a temp scratchpad for one test.
type env struct {
	t     *testing.T
	store string
	pad   string
	lim   limits
}

func newEnv(t *testing.T) *env {
	t.Helper()
	base := t.TempDir()
	// macOS temp folders sit behind a link; Capture wants the real path.
	base, err := filepath.EvalSymlinks(base)
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, store: filepath.Join(base, ".wipnote"), pad: filepath.Join(base, "scratchpad"), lim: defaultLimits()}
	for _, d := range []string{e.store, e.pad} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

// write puts content at rel in the scratchpad.
func (e *env) write(rel, content string) string {
	e.t.Helper()
	path := filepath.Join(e.pad, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return path
}

// touch moves a scratch file's time forward without changing its content.
func (e *env) touch(rel string) {
	e.t.Helper()
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(filepath.Join(e.pad, filepath.FromSlash(rel)), later, later); err != nil {
		e.t.Fatal(err)
	}
}

// capture runs one pass with the env's limits and returns the manifest.
func (e *env) capture(item string) (Manifest, Result) {
	e.t.Helper()
	res, err := capture(Request{StoreDir: e.store, SessionID: testSession, ScratchpadDir: e.pad, WorkItem: item}, e.lim)
	if err != nil {
		e.t.Fatalf("capture: %v", err)
	}
	m, err := Load(e.store, testSession)
	if err != nil {
		e.t.Fatalf("load manifest: %v", err)
	}
	return m, res
}

func (e *env) sessionDir() string { return filepath.Join(e.store, "scratch", testSession) }

// copyPath is where the saved copy of rel lives.
func (e *env) copyPath(rel string) string {
	return filepath.Join(e.sessionDir(), "files", filepath.FromSlash(rel))
}

func (e *env) hasCopy(rel string) bool {
	_, err := os.Lstat(e.copyPath(rel))
	return err == nil
}

// row finds the manifest row for path, or fails the test.
func row(t *testing.T, m Manifest, path string) Row {
	t.Helper()
	for _, r := range m.Files {
		if r.Path == path {
			return r
		}
	}
	t.Fatalf("no manifest row for %q in %+v", path, m.Files)
	return Row{}
}

func hasRow(m Manifest, path string) bool {
	for _, r := range m.Files {
		if r.Path == path {
			return true
		}
	}
	return false
}

// wantSkipped asserts path is listed with reason and has no copy.
func (e *env) wantSkipped(m Manifest, path, kind, reason string) {
	e.t.Helper()
	r := row(e.t, m, path)
	if r.Saved || r.SkipReason != reason || r.Kind != kind || r.SHA256 != "" {
		e.t.Errorf("row %q = %+v, want unsaved kind %q with skip reason %q and no hash", path, r, kind, reason)
	}
	if e.hasCopy(path) {
		e.t.Errorf("%q was copied into the store", path)
	}
}

// snapshot maps every path under dir to its size and time.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out[path] = info.ModTime().Format(time.RFC3339Nano) + " " + info.Mode().String()
		if !d.IsDir() {
			out[path] += " " + string(rune('0'+info.Size()%10))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
