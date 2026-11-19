package pluginbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// skillByteLimits caps each SKILL.md at its size when feat-9fd0cb31 landed.
// A slice that shrinks a skill lowers its limit in the same commit. A skill
// with no entry here has no room to grow, so a new skill needs a row first.
var skillByteLimits = map[string]int{
	"execute":                      8588,
	"guard-authoring":              4521,
	"report-issue":                 2687,
	"simplified-technical-english": 9022,
	"skills":                       1902,
	"ui-stills-verification":       7556,
}

// promptByteLimits caps each file under cmd/wipnote/prompts, same rule.
var promptByteLimits = map[string]int{
	"gemini-system.md":    4323,
	"research-routing.md": 571,
	"system-prompt.md":    3500,
	"worker-rules.md":     3072,
	"yolo-prompt.md":      5149,
}

// checkGuidanceSizes compares each guidance file under root with its limit and
// returns one message per file that is over, or has no limit at all.
func checkGuidanceSizes(root string, skills, prompts map[string]int) []string {
	var out []string
	skillsDir := filepath.Join(root, "plugin", "skills")
	entries, _ := os.ReadDir(skillsDir)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		out = append(out, checkOne(filepath.Join(skillsDir, e.Name(), "SKILL.md"), e.Name(), skills)...)
	}
	promptsDir := filepath.Join(root, "cmd", "wipnote", "prompts")
	files, _ := os.ReadDir(promptsDir)
	for _, f := range files {
		if f.IsDir() {
			continue
		}
		out = append(out, checkOne(filepath.Join(promptsDir, f.Name()), f.Name(), prompts)...)
	}
	return out
}

func checkOne(path, key string, limits map[string]int) []string {
	info, err := os.Stat(path)
	if err != nil {
		return nil // a skill directory without SKILL.md is not guidance
	}
	limit, ok := limits[key]
	if !ok {
		return []string{fmt.Sprintf("%s has no byte limit: add one to the table", path)}
	}
	if int(info.Size()) > limit {
		return []string{fmt.Sprintf("%s is %d bytes, over its limit of %d", path, info.Size(), limit)}
	}
	return nil
}

func writeSized(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("a", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGuidanceSize_SkillOverLimitFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "plugin", "skills", "demo", "SKILL.md"), 101)
	got := checkGuidanceSizes(root, map[string]int{"demo": 100}, nil)
	if len(got) != 1 || !strings.Contains(got[0], "101 bytes, over its limit of 100") {
		t.Fatalf("one byte over the limit must fail, got %v", got)
	}
	writeSized(t, filepath.Join(root, "plugin", "skills", "demo", "SKILL.md"), 100)
	if got := checkGuidanceSizes(root, map[string]int{"demo": 100}, nil); len(got) != 0 {
		t.Fatalf("a file at its limit must pass, got %v", got)
	}
}

func TestGuidanceSize_PromptOverLimitFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "cmd", "wipnote", "prompts", "p.md"), 51)
	got := checkGuidanceSizes(root, nil, map[string]int{"p.md": 50})
	if len(got) != 1 || !strings.Contains(got[0], "51 bytes, over its limit of 50") {
		t.Fatalf("a prompt one byte over must fail, got %v", got)
	}
}

func TestGuidanceSize_UnlistedFileFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "plugin", "skills", "new", "SKILL.md"), 10)
	got := checkGuidanceSizes(root, map[string]int{}, nil)
	if len(got) != 1 || !strings.Contains(got[0], "has no byte limit") {
		t.Fatalf("a skill with no limit must fail, got %v", got)
	}
}

func TestGuidanceSize_RealTreeWithinLimits(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	for _, msg := range checkGuidanceSizes(root, skillByteLimits, promptByteLimits) {
		t.Error(msg)
	}
}

var evalSkillPrefix = regexp.MustCompile(`(?m)^description:\s*"?([a-z0-9-]+):`)

// TestSkillEvalCoverage prints the skills that no eval case names. It never
// fails: most uncovered skills are deleted later in plan-2622b3f7.
func TestSkillEvalCoverage(t *testing.T) {
	skillsDir, dirs := sharedSkillDirs(t)
	evalsDir := filepath.Join(filepath.Dir(skillsDir), "evals")
	covered := map[string]bool{}
	cases, _ := filepath.Glob(filepath.Join(evalsDir, "*", "prompt.md"))
	for _, c := range cases {
		raw, err := os.ReadFile(c)
		if err != nil {
			continue
		}
		if m := evalSkillPrefix.FindStringSubmatch(string(raw)); m != nil {
			covered[m[1]] = true
		}
	}
	var missing []string
	for _, d := range dirs {
		if !covered[d] {
			missing = append(missing, d)
		}
	}
	sort.Strings(missing)
	t.Logf("skills with no eval case (%d of %d): %s", len(missing), len(dirs), strings.Join(missing, ", "))
}
