package pluginbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInlinePreloadedSkills guards the Codex review finding on #209: Codex and
// Antigravity drop `skills:`, so the build must carry the skill body into the
// agent. A coder agent gets the worker rules block.
func TestInlinePreloadedSkills(t *testing.T) {
	skills := t.TempDir()
	if err := os.MkdirAll(filepath.Join(skills, "other"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skills, "other", "SKILL.md"),
		[]byte("---\nname: other\n---\n\n# Other\n\nNever hand-edit .wipnote/.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rules := filepath.Join(t.TempDir(), "worker-rules.md")
	if err := os.WriteFile(rules, []byte("## Worker rules\n\nRule one.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	agent := []byte("---\nname: x\ndescription: d\nskills:\n  - wipnote:other\n---\n\n# Agent\n\nBody.\n")
	out, err := inlinePreloadedSkills(agent, skills, rules)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"name: x", "# Agent", "## Preloaded skill: other", "Never hand-edit .wipnote/."} {
		if !strings.Contains(s, want) {
			t.Errorf("inlined agent missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Worker rules") {
		t.Errorf("an agent with no replaced skill must not get the block:\n%s", s)
	}
	if strings.Count(s, "---\n") != 2 {
		t.Errorf("skill frontmatter must not leak into the agent:\n%s", s)
	}

	coder := []byte("---\nname: feature-coder\ndescription: d\n---\nBody.\n")
	cg, err := inlinePreloadedSkills(coder, skills, rules)
	if err != nil || strings.Count(string(cg), "## Worker rules") != 1 {
		t.Errorf("a coder agent with no skills field must get the block once, err=%v:\n%s", err, cg)
	}

	plain := []byte("---\nname: y\ndescription: d\n---\nBody.\n")
	if got, _ := inlinePreloadedSkills(plain, skills, rules); string(got) != string(plain) {
		t.Errorf("agent without skills must be unchanged, got %q", got)
	}
	if _, err := inlinePreloadedSkills([]byte("---\nname: z\nskills: [missing]\n---\nB\n"), skills, rules); err == nil {
		t.Error("a listed skill that does not exist must fail the build")
	}
}

// TestLiveNonClaudeCodersCarryWorkerRules checks the generated trees: every
// coder agent must carry the worker rules and not the two skills they replace.
func TestLiveNonClaudeCodersCarryWorkerRules(t *testing.T) {
	root := filepath.Join("..", "packages")
	for _, path := range []string{
		filepath.Join(root, "codex-marketplace", ".agents", "plugins", "wipnote", "agents", "wipnote-feature-coder.toml"),
		filepath.Join(root, "antigravity-extension", "agents", "feature-coder.md"),
	} {
		assertCarriesRulesNotSkills(t, path)
	}
}

// TestClaudeAgentsCarryNoSkillsField checks the plugin's agent files hold no
// skills preload, because SubagentStart delivers the block (feat-0b0112a4).
func TestClaudeAgentsCarryNoSkillsField(t *testing.T) {
	root := repositoryRootForTest(t)
	files, _ := filepath.Glob(filepath.Join(root, "plugin", "agents", "*.md"))
	if len(files) == 0 {
		t.Fatal("no agent files found")
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		fm, _, _, err := parseAgentFrontmatter(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, has := fm["skills"]; has {
			t.Errorf("%s still has a skills field", f)
		}
	}
}
