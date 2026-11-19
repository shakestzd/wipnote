package pluginbuild

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// skillFilesUnder returns every file below dir. An absent dir is the one
// expected absence and gives no files. Any other walk error is returned.
func skillFilesUnder(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	return files, err
}

func TestSkillFilesUnder_FindsFiles(t *testing.T) {
	root := t.TempDir()
	if got, err := skillFilesUnder(filepath.Join(root, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("a missing folder holds no files and no error, got %v, %v", got, err)
	}
	p := filepath.Join(root, "a", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := skillFilesUnder(root); err != nil || len(got) != 1 || got[0] != p {
		t.Fatalf("want [%s], got %v, %v", p, got, err)
	}
}

func TestSkillFilesUnder_ReportsReadError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A file where a folder is expected fails with ENOTDIR, not absence.
	if _, err := skillFilesUnder(filepath.Join(root, "f", "sub")); err == nil {
		t.Fatal("a non-absence read error must be returned")
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
		files, err := skillFilesUnder(filepath.Join(root, "plugin", dir))
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			t.Errorf("plugin/%s should hold no file, found %s", dir, f)
		}
	}
}
