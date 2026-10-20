package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// writeMod lays out <dir>/<name>/.claude-plugin/plugin.json.
func writeMod(t *testing.T, dir, name string) {
	t.Helper()
	manifest := filepath.Join(dir, name, ".claude-plugin", "plugin.json")
	if err := os.MkdirAll(filepath.Dir(manifest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte(`{"name":"`+name+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveModsDirBesideThePlugin(t *testing.T) {
	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", "")
	share := t.TempDir()
	plugin := filepath.Join(share, "plugin")
	if err := os.MkdirAll(plugin, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("no mods folder: got %q, want none", got)
	}
	if err := os.MkdirAll(filepath.Join(share, "mods", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("a mods folder with no mod in it: got %q, want none", got)
	}
	writeMod(t, filepath.Join(share, "mods"), "wipnote-orchestra")
	if got, want := resolveModsDir(plugin), filepath.Join(share, "mods"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if got := resolveModsDir(""); got != "" {
		t.Errorf("no plugin dir: got %q, want none", got)
	}
}

func TestResolveModsDirOverrideAndOptOut(t *testing.T) {
	share := t.TempDir()
	writeMod(t, filepath.Join(share, "mods"), "codex-herdr-pane")
	plugin := filepath.Join(share, "plugin")
	other := t.TempDir()
	writeMod(t, other, "mine")

	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", other)
	if got := resolveModsDir(plugin); got != other {
		t.Errorf("override: got %q, want %q", got, other)
	}
	t.Setenv("WIPNOTE_MODS_DIR", t.TempDir())
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("override with no mod: got %q, want none", got)
	}
	t.Setenv("WIPNOTE_MODS_DIR", "")
	t.Setenv("WIPNOTE_NO_MODS", "1")
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("opted out: got %q, want none", got)
	}
	t.Setenv("WIPNOTE_NO_MODS", "0")
	if got := resolveModsDir(plugin); got == "" {
		t.Errorf("WIPNOTE_NO_MODS=0 must still load the mods")
	}
}

func TestBuildClaudeArgsLoadsModsAfterThePlugin(t *testing.T) {
	args := buildClaudeArgs(LaunchOpts{PluginDir: "/s/plugin", ModsDir: "/s/mods"}, "")
	want := []string{"--plugin-dir", "/s/plugin", "--plugin-dir", "/s/mods"}
	if !slices.Equal(args, want) {
		t.Errorf("args = %v, want %v", args, want)
	}
}

// The repository's own mods/ folder is what --dev and the release ship.
func TestRepoModsFolderHoldsMods(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if !isModsTree(filepath.Join(root, "mods")) {
		t.Errorf("%s/mods holds no mod", root)
	}
}
