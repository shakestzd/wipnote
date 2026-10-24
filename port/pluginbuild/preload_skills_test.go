package pluginbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInlinePreloadedSkills guards the Codex review finding on #209: Codex and
// Antigravity drop `skills:`, so the build must carry the skill body into the
// agent or those agents lose the shared safety and gate rules.
func TestInlinePreloadedSkills(t *testing.T) {
	skills := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skills, "agent-context"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "agent-context", "SKILL.md"),
		[]byte("---\nname: agent-context\n---\n\n# Shared Agent Context\n\nNever hand-edit .wipnote/.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := []byte("---\nname: x\ndescription: d\nskills:\n  - wipnote:agent-context\n---\n\n# Agent\n\nBody.\n")
	out, err := inlinePreloadedSkills(agent, skills)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"name: x", "# Agent", "## Preloaded skill: agent-context", "Never hand-edit .wipnote/."} {
		if !strings.Contains(s, want) {
			t.Errorf("inlined agent missing %q:\n%s", want, s)
		}
	}
	if strings.Count(s, "---\n") != 2 {
		t.Errorf("skill frontmatter must not leak into the agent:\n%s", s)
	}

	plain := []byte("---\nname: y\ndescription: d\n---\nBody.\n")
	if got, _ := inlinePreloadedSkills(plain, skills); string(got) != string(plain) {
		t.Errorf("agent without skills must be unchanged, got %q", got)
	}
	if _, err := inlinePreloadedSkills([]byte("---\nname: z\nskills: [missing]\n---\nB\n"), skills); err == nil {
		t.Error("a listed skill that does not exist must fail the build")
	}
}

// TestLiveNonClaudeCodersCarryAgentContext checks the generated trees: every
// agent that preloads agent-context on Claude must contain its rules on the
// targets that cannot preload.
func TestLiveNonClaudeCodersCarryAgentContext(t *testing.T) {
	root := filepath.Join("..", "packages")
	for _, path := range []string{
		filepath.Join(root, "codex-marketplace", ".agents", "plugins", "wipnote", "agents", "wipnote-feature-coder.toml"),
		filepath.Join(root, "antigravity-extension", "agents", "feature-coder.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !strings.Contains(string(data), "Preloaded skill: agent-context") {
			t.Errorf("%s does not carry the agent-context rules", path)
		}
	}
}
