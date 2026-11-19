package pluginbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// promptByteLimits caps each file under cmd/wipnote/prompts, same rule.
var promptByteLimits = map[string]int{
	"gemini-system.md":    4323,
	"research-routing.md": 571,
	"system-prompt.md":    3500,
	"worker-rules.md":     3072,
	"yolo-prompt.md":      5149,
}

// checkGuidanceSizes compares each prompt under root with its limit and
// returns one message per file that is over, or has no limit at all. It also
// returns one message for every file left under plugin/skills, because no
// skill ships (feat-9796e97f).
func checkGuidanceSizes(root string, prompts map[string]int) []string {
	var out []string
	for _, f := range skillFilesUnder(filepath.Join(root, "plugin", "skills")) {
		out = append(out, fmt.Sprintf("%s is a skill file, but no skill ships", f))
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
		return nil
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

func TestGuidanceSize_SkillFileFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "plugin", "skills", "demo", "SKILL.md"), 10)
	got := checkGuidanceSizes(root, nil)
	if len(got) != 1 || !strings.Contains(got[0], "no skill ships") {
		t.Fatalf("a skill file must fail, got %v", got)
	}
}

func TestGuidanceSize_PromptOverLimitFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "cmd", "wipnote", "prompts", "p.md"), 51)
	got := checkGuidanceSizes(root, map[string]int{"p.md": 50})
	if len(got) != 1 || !strings.Contains(got[0], "51 bytes, over its limit of 50") {
		t.Fatalf("a prompt one byte over must fail, got %v", got)
	}
}

func TestGuidanceSize_UnlistedPromptFails(t *testing.T) {
	root := t.TempDir()
	writeSized(t, filepath.Join(root, "cmd", "wipnote", "prompts", "new.md"), 10)
	got := checkGuidanceSizes(root, map[string]int{})
	if len(got) != 1 || !strings.Contains(got[0], "has no byte limit") {
		t.Fatalf("a prompt with no limit must fail, got %v", got)
	}
}

func TestGuidanceSize_RealTreeWithinLimits(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	root := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	for _, msg := range checkGuidanceSizes(root, promptByteLimits) {
		t.Error(msg)
	}
}
