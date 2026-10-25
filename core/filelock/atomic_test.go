package filelock

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestWriteFileAtomic_ReplacesAndCleansUp pins the success side: new and
// existing files get the data, a new file gets perm, and no temp is left.
func TestWriteFileAtomic_ReplacesAndCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.html")
	for _, data := range []string{"first", "second, longer than first", ""} {
		if err := WriteFileAtomic(path, []byte(data), 0o600); err != nil {
			t.Fatalf("WriteFileAtomic(%q): %v", data, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != data {
			t.Fatalf("content = %q, %v; want %q", got, err, data)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("perm = %v, %v; want 0600", info.Mode().Perm(), err)
	}
	assertOnlyFile(t, dir, "node.html")
}

// TestWriteFileAtomic_FailureLeavesTarget pins the failure side: a missing
// parent directory is an error (it is not created), and a rename that fails
// keeps the old target and removes the temp file.
func TestWriteFileAtomic_FailureLeavesTarget(t *testing.T) {
	dir := t.TempDir()
	if err := WriteFileAtomic(filepath.Join(dir, "missing", "f"), []byte("x"), 0o644); err == nil {
		t.Error("write into a missing directory returned no error")
	}
	target := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0o644); err == nil {
		t.Error("rename over a non-empty directory returned no error")
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Errorf("target changed after failed write: %v, %v", info, err)
	}
	assertOnlyFile(t, dir, "target")
}

// TestWriteFileAtomic_ConcurrentWriters pins that concurrent writers to one
// path each use their own temp file and the result is one complete payload.
func TestWriteFileAtomic_ConcurrentWriters(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	payloads := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd"}
	var wg sync.WaitGroup
	for _, p := range payloads {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := WriteFileAtomic(path, []byte(p), 0o644); err != nil {
				t.Errorf("concurrent write: %v", err)
			}
		}(p)
	}
	wg.Wait()
	got, _ := os.ReadFile(path)
	ok := false
	for _, p := range payloads {
		ok = ok || string(got) == p
	}
	if !ok {
		t.Errorf("content %q is not one complete payload", got)
	}
	assertOnlyFile(t, dir, "f")
}

func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != name {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("dir holds %v, want only %s", names, name)
	}
}
