package pluginbuild

import (
	"os"
	"path/filepath"
	"testing"
)

// The delegate guide is deleted (feat-8f910039): its steps live in
// wipnote delegate --help and wipnote delegate profiles. No built tree may
// carry it, and the source folder must be gone.
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

func TestBuildPorts_NoDelegateSkillAnywhere(t *testing.T) {
	m, repoRoot := liveRepo(t)
	out := t.TempDir()
	emitted := map[string]string{}
	for name, adapter := range map[string]Adapter{"claude": claudeAdapter{}, "codex": codexAdapter{}, "antigravity": antigravityAdapter{}} {
		dir := filepath.Join(out, name)
		if err := adapter.Emit(m, repoRoot, dir); err != nil {
			t.Fatalf("emit %s: %v", name, err)
		}
		emitted[name] = dir
	}
	var dirs []string
	dirs = append(dirs, filepath.Join(repoRoot, m.AssetSources.Skills, delegateSkill))
	for name, dir := range emitted {
		skills := "skills"
		if name == "codex" {
			skills = filepath.Join(m.Targets["codex"].PluginSubdir, "skills")
		}
		dirs = append(dirs, filepath.Join(dir, skills, delegateSkill))
	}
	for _, name := range []string{"codex", "antigravity"} {
		skills := "skills"
		if name == "codex" {
			skills = filepath.Join(m.Targets["codex"].PluginSubdir, "skills")
		}
		dirs = append(dirs, filepath.Join(repoRoot, m.Targets[name].OutDir, skills, delegateSkill))
	}
	for _, dir := range dirs {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("the deleted delegate guide is still at %s (stat err %v)", dir, err)
		}
	}
	// Other skills still ship.
	if _, err := os.Stat(filepath.Join(emitted["claude"], "skills", "execute", "SKILL.md")); err != nil {
		t.Errorf("claude tree lost an unrelated skill: %v", err)
	}
}
