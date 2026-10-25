package pluginbuild

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func writeAssetFixture(t *testing.T, root string, files ...string) {
	t.Helper()
	for _, f := range files {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func listTree(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// TestAssetTreeCopies_SharedWalk pins the walk all three asset copies share:
// .archived subtrees never ship, codex-override files ship only in the
// generic copy, and every other file lands at the same relative path.
func TestAssetTreeCopies_SharedWalk(t *testing.T) {
	src := t.TempDir()
	override := "agent.codex.md"
	writeAssetFixture(t, src, "a.md", "sub/b.md", ".archived/old.md", "sub/.archived/old2.md", "sub/"+override)
	roles := map[string]struct{}{}

	copies := map[string]func(src, dst string) error{
		"generic": copyAssetTree,
		"codex":   func(s, d string) error { return copyAssetTreeCodex(s, d, roles) },
		"gemini":  func(s, d string) error { return copyAssetTreeGemini(s, d, roles) },
	}
	for name, copyFn := range copies {
		dst := t.TempDir()
		if err := copyFn(src, dst); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		want := []string{"a.md", "sub/b.md"}
		if name == "generic" {
			want = append(want, "sub/"+override)
		}
		sort.Strings(want)
		if got := listTree(t, dst); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s copied %v, want %v", name, got, want)
		}
	}
}

// TestAssetTreeCopies_SourceEdgeCases pins the shared preconditions: a
// missing source is a no-op, a file source is an error, and copying a
// directory onto itself leaves it untouched.
func TestAssetTreeCopies_SourceEdgeCases(t *testing.T) {
	roles := map[string]struct{}{}
	copies := map[string]func(src, dst string) error{
		"generic": copyAssetTree,
		"codex":   func(s, d string) error { return copyAssetTreeCodex(s, d, roles) },
		"gemini":  func(s, d string) error { return copyAssetTreeGemini(s, d, roles) },
	}
	for name, copyFn := range copies {
		base := t.TempDir()
		missingDst := filepath.Join(base, "dst")
		if err := copyFn(filepath.Join(base, "missing"), missingDst); err != nil {
			t.Errorf("%s: missing source: %v", name, err)
		}
		if _, err := os.Stat(missingDst); !os.IsNotExist(err) {
			t.Errorf("%s: missing source created the destination", name)
		}
		file := filepath.Join(base, "file.md")
		writeAssetFixture(t, base, "file.md")
		if err := copyFn(file, filepath.Join(base, "out")); err == nil {
			t.Errorf("%s: file source returned no error", name)
		}
		same := filepath.Join(base, "same")
		writeAssetFixture(t, same, "keep.md")
		if err := copyFn(same, same); err != nil {
			t.Errorf("%s: same-dir copy: %v", name, err)
		}
		if data, _ := os.ReadFile(filepath.Join(same, "keep.md")); string(data) != "x" {
			t.Errorf("%s: same-dir copy changed keep.md to %q", name, data)
		}
	}
}
