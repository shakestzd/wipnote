package pluginbuild

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyAssetTreesSkipArchived(t *testing.T) {
	src := t.TempDir()
	for _, rel := range []string{"live/SKILL.md", ".archived/old/SKILL.md"} {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copiers := map[string]func(src, dst string) error{
		"claude":      copyAssetTree,
		"codex":       func(s, d string) error { return copyAssetTreeCodex(s, d, nil) },
		"antigravity": func(s, d string) error { return copyAssetTreeGemini(s, d, nil) },
	}
	for name, copyTree := range copiers {
		dst := t.TempDir()
		if err := copyTree(src, dst); err != nil {
			t.Fatalf("%s: copy: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dst, "live", "SKILL.md")); err != nil {
			t.Errorf("%s: live skill was not copied: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dst, ".archived")); !os.IsNotExist(err) {
			t.Errorf("%s: .archived was copied into the target tree", name)
		}
	}
}
