package pluginbuild

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// skillFilesUnder returns every file below dir, or nil when dir is absent.
func skillFilesUnder(dir string) []string {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files
}

func TestSkillFilesUnder_FindsFiles(t *testing.T) {
	root := t.TempDir()
	if got := skillFilesUnder(filepath.Join(root, "missing")); len(got) != 0 {
		t.Fatalf("a missing folder holds no files, got %v", got)
	}
	p := filepath.Join(root, "a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := skillFilesUnder(root); len(got) != 1 || got[0] != p {
		t.Fatalf("want [%s], got %v", p, got)
	}
}

// TestNoSkillOrCommandSource checks the source tree ships no skill, no skills
// index and no slash command (feat-9796e97f). Guidance lives in the system
// prompt, the worker rules and each command's --help.
func TestNoSkillOrCommandSource(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	for _, dir := range []string{"skills", "commands"} {
		for _, f := range skillFilesUnder(filepath.Join(root, "plugin", dir)) {
			t.Errorf("plugin/%s should hold no file, found %s", dir, f)
		}
	}
}
