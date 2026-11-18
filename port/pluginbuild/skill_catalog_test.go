package pluginbuild

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// modelVisibleSkills are the shared skills whose descriptions stay in the
// model's skill list. Every other skill carries disable-model-invocation and is
// reached through its slash command or the wipnote:skills catalog, which keeps
// the per-turn listing small.
var modelVisibleSkills = map[string]bool{
	"agent-context":      true, // preloaded into coder agents via skills:
	"code-quality-skill": true, // preloaded into coder agents via skills:
	"execute":            true,
	"skills":             true, // the catalog itself
}

// sharedSkillDirs lists plugin/skills/<dir> entries that hold a SKILL.md.
func sharedSkillDirs(t *testing.T) (string, []string) {
	t.Helper()
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	skillsDir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(manifestPath))), "plugin", "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		t.Fatalf("read %s: %v", skillsDir, err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillsDir, e.Name(), "SKILL.md")); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	return skillsDir, dirs
}

// frontmatter returns a SKILL.md's YAML frontmatter block, or "".
func frontmatter(raw string) string {
	if !strings.HasPrefix(raw, "---\n") {
		return ""
	}
	end := strings.Index(raw[4:], "\n---")
	if end < 0 {
		return ""
	}
	return raw[4 : 4+end]
}

func TestSkillVisibilityMatchesCoreSet(t *testing.T) {
	skillsDir, dirs := sharedSkillDirs(t)
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(skillsDir, dir, "SKILL.md"))
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		hidden := strings.Contains(frontmatter(string(raw)), "disable-model-invocation: true")
		if modelVisibleSkills[dir] && hidden {
			t.Errorf("%s is in the model-visible core set but carries disable-model-invocation", dir)
		}
		if !modelVisibleSkills[dir] && !hidden {
			t.Errorf("%s is not in the core set: add disable-model-invocation: true, or add it to modelVisibleSkills", dir)
		}
	}
}

func TestSkillCatalogListsEverySkill(t *testing.T) {
	skillsDir, dirs := sharedSkillDirs(t)
	raw, err := os.ReadFile(filepath.Join(skillsDir, "skills", "SKILL.md"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	catalog := string(raw)
	for _, dir := range dirs {
		if dir == "skills" {
			continue
		}
		if !strings.Contains(catalog, "| `"+dir+"` |") {
			t.Errorf("catalog skills/skills/SKILL.md has no row for %s (last column `%s`)", dir, dir)
		}
	}
}

// catalogRowSkill pulls the last-column skill dir out of a catalog table row.
var catalogRowSkill = regexp.MustCompile("(?m)^\\|.*\\| `([a-z0-9-]+)` \\|\\s*$")

// catalogStaleRows returns catalog rows whose skill dir is not in dirs.
func catalogStaleRows(catalog string, dirs []string) []string {
	have := map[string]bool{}
	for _, d := range dirs {
		have[d] = true
	}
	var stale []string
	for _, m := range catalogRowSkill.FindAllStringSubmatch(catalog, -1) {
		if !have[m[1]] {
			stale = append(stale, m[1])
		}
	}
	return stale
}

func TestSkillCatalog_StaleRowDetected(t *testing.T) {
	catalog := "| Need | Slash command | `<dir>` |\n|---|---|---|\n| A | `/x` | `plan` |\n| B | `/y` | `gone-skill` |\n"
	got := catalogStaleRows(catalog, []string{"plan"})
	if len(got) != 1 || got[0] != "gone-skill" {
		t.Fatalf("want [gone-skill], got %v", got)
	}
}

func TestSkillCatalog_NoStaleRows(t *testing.T) {
	skillsDir, dirs := sharedSkillDirs(t)
	raw, err := os.ReadFile(filepath.Join(skillsDir, "skills", "SKILL.md"))
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	for _, name := range catalogStaleRows(string(raw), dirs) {
		t.Errorf("catalog skills/skills/SKILL.md has a row for %s, but plugin/skills/%s/SKILL.md does not exist", name, name)
	}
}
