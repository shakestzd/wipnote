package pluginbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUIStillsVerificationSkillLandsInAllTargets guards GH-#165: the
// ui-stills-verification skill is a shared asset, so it must be emitted into
// every target tree and keep its two load-bearing sections — the mandatory
// PNG read-back and the no-real-data safety note — after translation.
func TestUIStillsVerificationSkillLandsInAllTargets(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	manifestPath, err := FindManifest(cwd)
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))

	const skill = "ui-stills-verification"
	source := filepath.Join(repoRoot, m.AssetSources.Skills, skill, "SKILL.md")
	assertUIStillsSkillContent(t, "source", source)

	outBase := t.TempDir()
	targets := []struct {
		name    string
		emit    func(string) error
		skillMD string
	}{
		{
			name:    "claude",
			emit:    func(out string) error { return (claudeAdapter{}).Emit(m, repoRoot, out) },
			skillMD: filepath.Join("skills", skill, "SKILL.md"),
		},
		{
			name:    "codex",
			emit:    func(out string) error { return (codexAdapter{}).Emit(m, repoRoot, out) },
			skillMD: filepath.Join(m.Targets["codex"].PluginSubdir, "skills", skill, "SKILL.md"),
		},
		{
			name:    "antigravity",
			emit:    func(out string) error { return (antigravityAdapter{}).Emit(m, repoRoot, out) },
			skillMD: filepath.Join("skills", skill, "SKILL.md"),
		},
	}
	for _, tgt := range targets {
		t.Run(tgt.name, func(t *testing.T) {
			out := filepath.Join(outBase, tgt.name)
			if err := tgt.emit(out); err != nil {
				t.Fatalf("emit %s: %v", tgt.name, err)
			}
			assertUIStillsSkillContent(t, tgt.name, filepath.Join(out, tgt.skillMD))
		})
	}

	// The checked-in generated trees must be current too, or a stale port
	// ships without the skill even though the emitter would produce it.
	for _, checkedIn := range []string{
		filepath.Join(repoRoot, m.Targets["codex"].OutDir, m.Targets["codex"].PluginSubdir, "skills", skill, "SKILL.md"),
		filepath.Join(repoRoot, m.Targets["antigravity"].OutDir, "skills", skill, "SKILL.md"),
	} {
		assertUIStillsSkillContent(t, "checked-in "+checkedIn, checkedIn)
	}
}

func assertUIStillsSkillContent(t *testing.T, label, path string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: read %s: %v", label, path, err)
	}
	body := string(raw)
	for _, want := range []string{
		"name: ui-stills-verification",
		"shot-scraper",
		"READ THE PNG BACK",
		"Read /abs/path/panel.png",
		"never capture real user data",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("%s: %s missing %q", label, path, want)
		}
	}
}
