package pluginbuild

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// manifestPathStrings returns every string value in a JSON document that
// starts with "./". Each one names a file or folder the manifest expects.
func manifestPathStrings(v any) []string {
	var out []string
	switch x := v.(type) {
	case string:
		if strings.HasPrefix(x, "./") {
			out = append(out, x)
		}
	case []any:
		for _, e := range x {
			out = append(out, manifestPathStrings(e)...)
		}
	case map[string]any:
		for _, e := range x {
			out = append(out, manifestPathStrings(e)...)
		}
	}
	return out
}

// danglingPaths walks every .json file under root and returns each "./" path
// that does not exist, resolved against the folder that holds the file.
// Codex's marketplace.json names its plugin from the marketplace root, which
// also sits two folders above it, so that file resolves against that root.
func danglingPaths(t *testing.T, root string) []string {
	t.Helper()
	var missing []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".json" {
			return nil
		}
		raw, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		var doc any
		if json.Unmarshal(raw, &doc) != nil {
			return nil
		}
		base := filepath.Dir(p)
		if filepath.Base(p) == "marketplace.json" && strings.Contains(p, ".agents") {
			base = filepath.Dir(filepath.Dir(base))
		}
		if filepath.Base(base) == ".codex-plugin" {
			base = filepath.Dir(base) // Codex paths resolve from the plugin root
		}
		for _, ref := range manifestPathStrings(doc) {
			if _, serr := os.Stat(filepath.Join(base, ref)); serr != nil {
				rel, _ := filepath.Rel(root, p)
				missing = append(missing, rel+" names "+ref)
			}
		}
		return nil
	})
	return missing
}

func TestDanglingPaths_Detects(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "plugin.json"), []byte(`{"skills":"./skills/","hooks":"./hooks.json"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := danglingPaths(t, root)
	if len(got) != 1 || got[0] != "plugin.json names ./skills/" {
		t.Fatalf("want only the missing skills folder, got %v", got)
	}
}

// TestBuildPorts_NoDanglingPaths builds each target and checks that every
// path its manifests name exists (feat-9796e97f). Deleting the last skills
// left Codex's plugin.json pointing at a skills folder that was gone.
func TestBuildPorts_NoDanglingPaths(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	for _, name := range []string{"claude", "codex", "antigravity"} {
		t.Run(name, func(t *testing.T) {
			a, err := Get(name)
			if err != nil {
				t.Fatalf("Get(%s): %v", name, err)
			}
			out := t.TempDir()
			if err := a.Emit(m, repoRoot, out); err != nil {
				t.Fatalf("emit %s: %v", name, err)
			}
			if got := danglingPaths(t, out); len(got) > 0 {
				t.Errorf("%s manifests name missing paths: %v", name, got)
			}
		})
	}
}
