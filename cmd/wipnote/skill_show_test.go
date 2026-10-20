package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

func writeSkillTree(t *testing.T, root, sentinel, skillsSubdir, body string) {
	t.Helper()
	for path, content := range map[string]string{
		filepath.Join(root, sentinel):                                       "{}",
		filepath.Join(root, skillsSubdir, "diagnose", "SKILL.md"):           body,
		filepath.Join(root, skillsSubdir, "plan", "INTERVIEW_RENDERING.md"): body + " ref",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestResolveSkillFilePicksHarnessTree(t *testing.T) {
	claudeRoot := t.TempDir()
	writeSkillTree(t, claudeRoot, filepath.Join(".claude-plugin", "plugin.json"), "skills", "claude")
	agRoot := t.TempDir()
	writeSkillTree(t, agRoot, "plugin.json", "skills", "antigravity")
	t.Setenv("WIPNOTE_PLUGIN_DIR", claudeRoot)
	t.Setenv("WIPNOTE_ANTIGRAVITY_DIR", agRoot)

	cases := []struct{ harness, file, want string }{
		{agent.HarnessClaude, "SKILL.md", "claude"},
		{agent.HarnessAntigravity, "SKILL.md", "antigravity"},
		{agent.HarnessAntigravity, "INTERVIEW_RENDERING.md", "antigravity ref"},
		{"", "SKILL.md", "claude"},
	}
	for _, c := range cases {
		dir := "diagnose"
		if c.file != "SKILL.md" {
			dir = "plan"
		}
		path, err := resolveSkillFile(c.harness, dir, c.file)
		if err != nil {
			t.Fatalf("%s %s: %v", c.harness, c.file, err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != c.want {
			t.Errorf("%s %s: got %q, want %q", c.harness, c.file, got, c.want)
		}
	}
}

func TestResolveSkillFileFallsBackToClaudeTree(t *testing.T) {
	claudeRoot := t.TempDir()
	writeSkillTree(t, claudeRoot, filepath.Join(".claude-plugin", "plugin.json"), "skills", "claude")
	t.Setenv("WIPNOTE_PLUGIN_DIR", claudeRoot)
	t.Setenv("WIPNOTE_CODEX_DIR", filepath.Join(t.TempDir(), "missing"))

	path, err := resolveSkillFile(agent.HarnessCodex, "diagnose", "SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(path, claudeRoot) {
		t.Errorf("path %s is not in the Claude tree %s", path, claudeRoot)
	}
}

func TestResolveSkillFileRejectsTraversal(t *testing.T) {
	for _, args := range [][2]string{{"../etc", "SKILL.md"}, {"diagnose", "../../x.md"}, {"diagnose", "run.sh"}, {"Diagnose", "SKILL.md"}} {
		if _, err := resolveSkillFile("", args[0], args[1]); err == nil {
			t.Errorf("resolveSkillFile(%q, %q) accepted an invalid name", args[0], args[1])
		}
	}
}
