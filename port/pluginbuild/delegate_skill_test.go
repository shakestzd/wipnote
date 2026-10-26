package pluginbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The delegate skill (plan-28b4a7f6 slice 28) ships to Claude Code and Codex
// only. Antigravity has no delegate workers in v1 (q-antigravity answered
// no), so its tree must not carry the skill.
const delegateSkill = "delegate"

// liveRepo loads the live manifest and its repo root, or skips.
func liveRepo(t *testing.T) (*Manifest, string) {
	t.Helper()
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return m, filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
}

func TestBuildPorts_DelegateSkillInClaudeAndCodex(t *testing.T) {
	m, repoRoot := liveRepo(t)
	outBase := t.TempDir()
	codexSkills := filepath.Join(m.Targets["codex"].PluginSubdir, "skills")
	targets := []struct {
		name      string
		adapter   Adapter
		skillsRel string
	}{
		{"claude", claudeAdapter{}, "skills"},
		{"codex", codexAdapter{}, codexSkills},
	}
	for _, tgt := range targets {
		t.Run(tgt.name, func(t *testing.T) {
			out := filepath.Join(outBase, tgt.name)
			if err := tgt.adapter.Emit(m, repoRoot, out); err != nil {
				t.Fatalf("emit %s: %v", tgt.name, err)
			}
			assertDelegateSkillContent(t, filepath.Join(out, tgt.skillsRel, delegateSkill, "SKILL.md"))
		})
	}
	// The checked-in trees must be current too.
	assertDelegateSkillContent(t, filepath.Join(repoRoot, m.AssetSources.Skills, delegateSkill, "SKILL.md"))
	assertDelegateSkillContent(t, filepath.Join(repoRoot, m.Targets["codex"].OutDir, codexSkills, delegateSkill, "SKILL.md"))
}

func TestBuildPorts_NoDelegateSkillInAntigravity(t *testing.T) {
	m, repoRoot := liveRepo(t)
	if _, err := os.Stat(filepath.Join(repoRoot, m.AssetSources.Skills, delegateSkill, "SKILL.md")); err != nil {
		t.Fatalf("source delegate skill missing, so this test proves nothing: %v", err)
	}
	out := t.TempDir()
	if err := (antigravityAdapter{}).Emit(m, repoRoot, out); err != nil {
		t.Fatalf("emit antigravity: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(out, "skills", delegateSkill),
		filepath.Join(repoRoot, m.Targets["antigravity"].OutDir, "skills", delegateSkill),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("antigravity tree must not carry the delegate skill: %s (stat err %v)", dir, err)
		}
	}
	// Other skills still ship.
	if _, err := os.Stat(filepath.Join(out, "skills", "execute", "SKILL.md")); err != nil {
		t.Errorf("antigravity tree lost an unrelated skill: %v", err)
	}
}

func assertDelegateSkillContent(t *testing.T, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(raw)
	for _, want := range []string{
		"name: delegate",
		"wipnote delegate run",
		"Release 2",
		"run page",
		"delegations-ledger.html",
		"codex-herdr-pane",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s missing %q", path, want)
		}
	}
}
