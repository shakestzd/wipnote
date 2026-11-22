package pluginbuild

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func liveRoot(t *testing.T) (string, *Manifest) {
	t.Helper()
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(manifestPath))), m
}

// readmeMatrixRows returns "event|handler|claude|codex|antigravity" for each
// row of the hook matrix table in the README text.
func readmeMatrixRows(readme string) []string {
	var rows []string
	if i := strings.Index(readme, "### Hook event matrix"); i >= 0 {
		readme = readme[i:]
	}
	if i := strings.Index(readme, "\n## "); i >= 0 {
		readme = readme[:i]
	}
	for _, line := range strings.Split(readme, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		cells := strings.Split(line, "|")
		cells = cells[1 : len(cells)-1]
		if len(cells) < 5 {
			continue
		}
		for i := range cells {
			cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
		}
		rows = append(rows, strings.Join(cells[:5], "|"))
	}
	sort.Strings(rows)
	return rows
}

func manifestMatrixRows(m *Manifest) []string {
	var rows []string
	for _, e := range m.Hooks.Events {
		has := map[string]bool{}
		for _, t := range e.Targets {
			has[t] = true
		}
		mark := func(t string) string {
			if has[t] {
				return "x"
			}
			return ""
		}
		rows = append(rows, strings.Join([]string{e.Name, e.Handler, mark("claude"), mark("codex"), mark("antigravity")}, "|"))
	}
	sort.Strings(rows)
	return rows
}

func TestReadmeHookMatrixMatchesManifest(t *testing.T) {
	root, m := liveRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "packages", "plugin-core", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readmeMatrixRows(string(data)), manifestMatrixRows(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("README hook matrix differs from manifest\nREADME:   %v\nmanifest: %v", got, want)
	}
}

func TestManifestAssetSourcesExist(t *testing.T) {
	root, m := liveRoot(t)
	a := m.AssetSources
	for name, rel := range map[string]string{"commands": a.Commands, "agents": a.Agents, "skills": a.Skills,
		"templates": a.Templates, "static": a.Static, "config": a.Config, "outputStyles": a.OutputStyles} {
		if rel == "" {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, rel)); err != nil || !info.IsDir() {
			t.Errorf("assetSources.%s names %s, which is not a folder: %v", name, rel, err)
		}
	}
}
