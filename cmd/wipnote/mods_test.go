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

// isolateModsLookup keeps the shared-tree fallback from finding this
// checkout's mods/ (from the working directory) or the real home's.
func isolateModsLookup(t *testing.T) string {
	t.Helper()
	t.Chdir(t.TempDir())
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

func TestResolveModsDirBesideThePlugin(t *testing.T) {
	isolateModsLookup(t)
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
}

// A plugin install with no mods beside it (one laid down before mods
// shipped) falls back to the shared-tree order, here ~/.local/share.
func TestResolveModsDirFallsBackToTheSharedTrees(t *testing.T) {
	t.Setenv("WIPNOTE_NO_MODS", "")
	t.Setenv("WIPNOTE_MODS_DIR", "")
	home := isolateModsLookup(t)
	shared := filepath.Join(home, ".local", "share", "wipnote", "mods")
	writeMod(t, shared, "codex-herdr-pane")
	stale := filepath.Join(t.TempDir(), "plugin") // no mods/ beside it
	if got := resolveModsDir(stale); got != shared {
		t.Errorf("got %q, want the shared tree %q", got, shared)
	}
	if got := resolveModsDir(""); got != shared {
		t.Errorf("no plugin dir: got %q, want the shared tree %q", got, shared)
	}
}

func TestResolveModsDirOverrideAndOptOut(t *testing.T) {
	isolateModsLookup(t)
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
	// A relative override comes back absolute: Claude starts in another folder.
	t.Chdir(filepath.Dir(other))
	t.Setenv("WIPNOTE_MODS_DIR", filepath.Base(other))
	if got := resolveModsDir(plugin); got != other {
		t.Errorf("relative override: got %q, want %q", got, other)
	}
	t.Setenv("WIPNOTE_MODS_DIR", t.TempDir())
	if got := resolveModsDir(plugin); got != "" {
		t.Errorf("override with no mod: got %q, want none (not the plugin's own)", got)
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

func TestMirrorModsTreeCopiesTrackedRuntimeFilesOnly(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "mods")
	if err := mirrorModsTree(root, dst); err != nil {
		t.Fatalf("mirrorModsTree: %v", err)
	}
	if !isModsTree(dst) {
		t.Fatalf("%s holds no mod after the mirror", dst)
	}
	for _, want := range []string{
		filepath.Join("codex-herdr-pane", "hooks", "hooks.json"),
		filepath.Join("codex-herdr-pane", "viewer", "codex.jq"),
		filepath.Join("wipnote-orchestra", "hooks", "register.tsx"),
	} {
		if _, err := os.Stat(filepath.Join(dst, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	err = filepath.Walk(dst, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dst, path)
		if !info.IsDir() && isModTestFile(rel) {
			t.Errorf("test file mirrored: %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMirrorModsTreeSkipsACheckoutWithoutMods(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "mods")
	writeMod(t, dst, "kept")
	if err := mirrorModsTree(t.TempDir(), dst); err != nil {
		t.Fatalf("no mods/ must not fail the build: %v", err)
	}
	if !isModsTree(dst) {
		t.Errorf("an existing mods tree must be left as it is")
	}
}

func TestIsModTestFile(t *testing.T) {
	for rel, want := range map[string]bool{
		"a/hooks/register.ts":      false,
		"a/hooks/register.test.ts": true,
		"a/test/run.sh":            true,
		"a/tests/band.test.tsx":    true,
		"a/viewer/codex_view.py":   false,
		"a/hooks/latest.ts":        false,
	} {
		if got := isModTestFile(rel); got != want {
			t.Errorf("isModTestFile(%q) = %v, want %v", rel, got, want)
		}
	}
}
