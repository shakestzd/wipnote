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
	if err := os.WriteFile(pluginJSON, []byte(`{"name":"wipnote","version":"0.1.0"}`), 0644); err != nil {
		t.Fatalf("failed to write plugin.json: %v", err)
	}
	return dir
}

// TestResolveProjectPluginDir_ProjectRootDetection tests that resolveProjectPluginDir
// walks up from CWD to find .wipnote/ and plugin/.
func TestResolveProjectPluginDir_ProjectRootDetection(t *testing.T) {
	// Create a fake project with .wipnote/ and plugin/
	tmpDir := t.TempDir()

	// Create .wipnote directory (marks project root)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".wipnote"), 0755); err != nil {
		t.Fatalf("failed to create .wipnote: %v", err)
	}

	// Create plugin directory structure
	pluginDir := filepath.Join(tmpDir, "plugin")
	createFakePlugin(t, pluginDir)

	// Clear env vars so earlier steps don't match
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	t.Setenv("WIPNOTE_PLUGIN_DIR", "")
	t.Setenv("HOME", filepath.Join(tmpDir, "fakehome")) // no marketplace

	// Change to project directory
	oldWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(oldWd)

	got := resolveProjectPluginDir()
	wantReal, _ := filepath.EvalSymlinks(pluginDir)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("resolveProjectPluginDir() = %q, want %q (project-root detection)", got, pluginDir)
	}
}

// TestResolveProjectPluginDir_FromSubdirectory tests that
// resolveProjectPluginDir can walk UP from a subdirectory to find the project root.
func TestResolveProjectPluginDir_FromSubdirectory(t *testing.T) {
	// Create a fake project with .wipnote/ and plugin/
	tmpDir := t.TempDir()

	// Create .wipnote directory (marks project root)
	if err := os.MkdirAll(filepath.Join(tmpDir, ".wipnote"), 0755); err != nil {
		t.Fatalf("failed to create .wipnote: %v", err)
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
	t.Setenv("WIPNOTE_PLUGIN_DIR", "")
	t.Setenv("HOME", filepath.Join(tmpDir, "fakehome"))

	// Change to subdirectory
	oldWd, _ := os.Getwd()
	os.Chdir(subDir)
	defer os.Chdir(oldWd)

	got := resolveProjectPluginDir()
	wantReal, _ := filepath.EvalSymlinks(pluginDir)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("resolveProjectPluginDir() from subdir = %q, want %q (should walk up)", got, pluginDir)
	}
}
