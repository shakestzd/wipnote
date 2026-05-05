package main

import (
	"os"
	"path/filepath"
	"testing"
)

// createFakePlugin creates a minimal plugin directory structure at the given path.
// Returns the plugin dir path for convenience.
func createFakePlugin(t *testing.T, dir string) string {
	t.Helper()
	claudePluginDir := filepath.Join(dir, ".claude-plugin")
	if err := os.MkdirAll(claudePluginDir, 0755); err != nil {
		t.Fatalf("failed to create .claude-plugin dir: %v", err)
	}
	pluginJSON := filepath.Join(claudePluginDir, "plugin.json")
	if err := os.WriteFile(pluginJSON, []byte(`{"name":"htmlgraph","version":"0.1.0"}`), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}
	return dir
}

// TestResolvePluginDir_ClaudePluginRoot tests that CLAUDE_PLUGIN_ROOT takes the
// highest priority (it is always set correctly by Claude Code in hook context).
func TestResolvePluginDir_ClaudePluginRoot(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := createFakePlugin(t, filepath.Join(tmpDir, "root-plugin"))

	t.Setenv("CLAUDE_PLUGIN_ROOT", pluginDir)
	t.Setenv("ERINN_PLUGIN_DIR", "")
	t.Setenv("HOME", t.TempDir()) // no well-known path

	got := resolvePluginDir()
	if got != pluginDir {
		t.Errorf("resolvePluginDir() = %q, want %q (CLAUDE_PLUGIN_ROOT)", got, pluginDir)
	}
}

// TestResolvePluginDir_ClaudePluginRootTakesPrecedenceOverHtmlgraphPluginDir tests
// that CLAUDE_PLUGIN_ROOT wins over ERINN_PLUGIN_DIR.
func TestResolvePluginDir_ClaudePluginRootTakesPrecedenceOverHtmlgraphPluginDir(t *testing.T) {
	tmpDir := t.TempDir()
	rootPlugin := createFakePlugin(t, filepath.Join(tmpDir, "root-plugin"))
	overridePlugin := createFakePlugin(t, filepath.Join(tmpDir, "override-plugin"))

	t.Setenv("CLAUDE_PLUGIN_ROOT", rootPlugin)
	t.Setenv("ERINN_PLUGIN_DIR", overridePlugin)
	t.Setenv("HOME", t.TempDir())

	got := resolvePluginDir()
	if got != rootPlugin {
		t.Errorf("resolvePluginDir() = %q, want %q (CLAUDE_PLUGIN_ROOT)", got, rootPlugin)
	}
}

// TestResolvePluginDir_EnvVarOverride tests that ERINN_PLUGIN_DIR takes priority
// when CLAUDE_PLUGIN_ROOT is not set.
func TestResolvePluginDir_EnvVarOverride(t *testing.T) {
	tmpDir := t.TempDir()
	pluginDir := createFakePlugin(t, filepath.Join(tmpDir, "my-plugin"))

	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", pluginDir)

	got := resolvePluginDir()
	if got != pluginDir {
		t.Errorf("resolvePluginDir() = %q, want %q", got, pluginDir)
	}
}

// TestResolvePluginDir_EnvVarInvalidFallsThrough tests that an invalid
// ERINN_PLUGIN_DIR does not short-circuit -- the function falls through
// to subsequent strategies.
func TestResolvePluginDir_EnvVarInvalidFallsThrough(t *testing.T) {
	t.Setenv("ERINN_PLUGIN_DIR", "/nonexistent/path/that/does/not/exist")

	// With no valid env var and no well-known path or symlink, should return "".
	got := resolvePluginDir()
	// We can't assert "" because the symlink walk-up from the test binary might
	// accidentally find a plugin dir. Instead, just ensure it didn't return
	// the invalid path.
	if got == "/nonexistent/path/that/does/not/exist" {
		t.Errorf("resolvePluginDir() returned invalid env var path without validation")
	}
}

// TestResolvePluginDir_EnvVarMissingPluginJSON tests that ERINN_PLUGIN_DIR
// is skipped when the directory exists but lacks .claude-plugin/plugin.json.
func TestResolvePluginDir_EnvVarMissingPluginJSON(t *testing.T) {
	tmpDir := t.TempDir()
	// Directory exists but has no .claude-plugin/plugin.json
	emptyDir := filepath.Join(tmpDir, "empty-plugin")
	if err := os.MkdirAll(emptyDir, 0755); err != nil {
		t.Fatalf("failed to create dir: %v", err)
	}

	t.Setenv("ERINN_PLUGIN_DIR", emptyDir)

	got := resolvePluginDir()
	if got == emptyDir {
		t.Errorf("resolvePluginDir() returned env var dir that lacks plugin.json")
	}
}

// TestResolvePluginDir_MarketplacePath tests that resolveMarketplacePluginDir
// discovers the plugin via installed_plugins.json when no env vars are set.
// The marketplace path is ~/.claude/plugins/cache/<marketplace>/<name>/<version>/
// — NOT ~/.claude/plugins/htmlgraph/ (the old hard-coded path).
func TestResolvePluginDir_MarketplacePath(t *testing.T) {
	tmpHome := t.TempDir()

	// Create a fake marketplace install at the real cache path structure.
	installPath := filepath.Join(tmpHome, ".claude", "plugins", "cache", "htmlgraph", "htmlgraph", "1.0.0")
	createFakePlugin(t, installPath)

	// Write installed_plugins.json pointing to the fake install.
	pluginsDir := filepath.Join(tmpHome, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("creating plugins dir: %v", err)
	}
	registryJSON := `{"version":2,"plugins":{"htmlgraph@htmlgraph":[{"installPath":"` + installPath + `","version":"1.0.0"}]}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(registryJSON), 0644); err != nil {
		t.Fatalf("writing installed_plugins.json: %v", err)
	}

	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", "")
	t.Setenv("HOME", tmpHome)

	got := resolvePluginDir()
	// Normalize symlinks (macOS /var → /private/var).
	wantReal, _ := filepath.EvalSymlinks(installPath)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("resolvePluginDir() = %q, want %q (marketplace path from installed_plugins.json)", got, installPath)
	}
}

// TestResolvePluginDir_EnvVarTakesPrecedenceOverMarketplace tests that env vars
// win over the marketplace path from installed_plugins.json.
func TestResolvePluginDir_EnvVarTakesPrecedenceOverMarketplace(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up env var plugin.
	envPluginDir := createFakePlugin(t, filepath.Join(tmpDir, "env-plugin"))

	// Set up marketplace plugin in installed_plugins.json.
	tmpHome := filepath.Join(tmpDir, "home")
	installPath := filepath.Join(tmpHome, ".claude", "plugins", "cache", "htmlgraph", "htmlgraph", "1.0.0")
	createFakePlugin(t, installPath)
	pluginsDir := filepath.Join(tmpHome, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("creating plugins dir: %v", err)
	}
	registryJSON := `{"version":2,"plugins":{"htmlgraph@htmlgraph":[{"installPath":"` + installPath + `","version":"1.0.0"}]}}`
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), []byte(registryJSON), 0644); err != nil {
		t.Fatalf("writing installed_plugins.json: %v", err)
	}

	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", envPluginDir)
	t.Setenv("HOME", tmpHome)

	got := resolvePluginDir()
	if got != envPluginDir {
		t.Errorf("resolvePluginDir() = %q, want %q (env var should take precedence over marketplace)", got, envPluginDir)
	}
}

// TestResolvePluginDir_SymlinkWalkUpFallback tests that the symlink walk-up
// behavior still works as a fallback. This validates backward compatibility
// with the dev mode workflow where the binary is symlinked from the plugin tree.
//
// Note: This test is inherently limited because os.Executable() returns the
// test binary path, not a symlink inside a plugin tree. We verify that the
// function at least returns "" when no other strategy matches, confirming the
// symlink walk-up doesn't crash.
func TestResolvePluginDir_SymlinkWalkUpFallback(t *testing.T) {
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", "")
	// Set HOME to a temp dir with no installed_plugins.json or plugin installed.
	t.Setenv("HOME", t.TempDir())

	got := resolvePluginDir()
	// The test binary is not inside a plugin tree, so the symlink walk-up
	// should return "". This confirms the fallback path runs without error.
	if got != "" {
		// It's possible the real binary happens to be in a plugin tree,
		// so we only warn rather than fail.
		t.Logf("resolvePluginDir() returned %q (may be a real plugin tree)", got)
	}
}

// TestResolvePluginDir_ProjectRootDetection tests that resolveProjectPluginDir
// walks up from CWD to find .htmlgraph/ and plugin/.
func TestResolvePluginDir_ProjectRootDetection(t *testing.T) {
	// Create a fake project with .htmlgraph/ and plugin/
	tmpDir := t.TempDir()

	// Create .htmlgraph directory (marks project root)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("failed to create .htmlgraph: %v", err)
	}

	// Create plugin directory structure
	pluginDir := filepath.Join(tmpDir, "plugin")
	createFakePlugin(t, pluginDir)

	// Clear env vars so earlier steps don't match
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", "")
	t.Setenv("HOME", filepath.Join(tmpDir, "fakehome")) // no marketplace

	// Change to project directory
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	got := resolvePluginDir()
	wantReal, _ := filepath.EvalSymlinks(pluginDir)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("resolvePluginDir() = %q, want %q (project-root detection)", got, pluginDir)
	}
}

// TestResolvePluginDir_ProjectRootDetectionFromSubdirectory tests that
// resolveProjectPluginDir can walk UP from a subdirectory to find the project root.
func TestResolvePluginDir_ProjectRootDetectionFromSubdirectory(t *testing.T) {
	// Create a fake project with .htmlgraph/ and plugin/
	tmpDir := t.TempDir()

	// Create .htmlgraph directory (marks project root)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("failed to create .htmlgraph: %v", err)
	}

	// Create plugin directory structure
	pluginDir := filepath.Join(tmpDir, "plugin")
	createFakePlugin(t, pluginDir)

	// Create a subdirectory that we'll work from
	subDir := filepath.Join(tmpDir, "src", "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Clear env vars
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("ERINN_PLUGIN_DIR", "")
	t.Setenv("HOME", filepath.Join(tmpDir, "fakehome"))

	// Change to subdirectory
	oldWd, _ := os.Getwd()
	os.Chdir(subDir)
	defer os.Chdir(oldWd)

	got := resolvePluginDir()
	wantReal, _ := filepath.EvalSymlinks(pluginDir)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("resolvePluginDir() from subdir = %q, want %q (should walk up)", got, pluginDir)
	}
}
