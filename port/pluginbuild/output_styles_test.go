package pluginbuild

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOutputStylesShipOnlyToClaude pins the Claude-only output-styles asset:
// Claude Code loads plugin output styles from output-styles/, while Codex and
// Antigravity have no such surface, so their trees must not carry the dir.
func TestOutputStylesShipOnlyToClaude(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	manifestPath, err := FindManifest(cwd)
	if err != nil {
		t.Fatalf("FindManifest: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.AssetSources.OutputStyles == "" {
		t.Fatal("manifest assetSources.outputStyles is empty")
	}

	claudeOut := t.TempDir()
	if err := (claudeAdapter{}).Emit(m, repoRoot, claudeOut); err != nil {
		t.Fatalf("claude Emit: %v", err)
	}
	style, err := os.ReadFile(filepath.Join(claudeOut, "output-styles", "plain-english.md"))
	if err != nil {
		t.Fatalf("claude tree missing output-styles/plain-english.md: %v", err)
	}
	fm, _, ok := splitFrontmatter(style)
	if !ok {
		t.Fatal("plain-english.md has no frontmatter")
	}
	for _, want := range []string{"name: Plain English", "keep-coding-instructions: true"} {
		if !strings.Contains(fm, want) {
			t.Errorf("plain-english.md frontmatter missing %q", want)
		}
	}

	for name, a := range map[string]Adapter{"codex": codexAdapter{}, "antigravity": antigravityAdapter{}} {
		out := t.TempDir()
		if err := a.Emit(m, repoRoot, out); err != nil {
			t.Fatalf("%s Emit: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(out, "output-styles")); !os.IsNotExist(err) {
			t.Errorf("%s tree has output-styles/ (err=%v); it is Claude-only", name, err)
		}
	}
}
