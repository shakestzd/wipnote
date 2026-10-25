package main

import (
	"os"
	"path/filepath"
)

// resolveProjectPluginDir walks up from CWD looking for a directory containing
// .wipnote/ and plugin/.claude-plugin/plugin.json. Returns the
// plugin dir path or "" if not found.
func resolveProjectPluginDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return resolveProjectPluginDirFrom(cwd)
}

// resolveProjectPluginDirFrom walks up from startDir (at most 5 levels) looking
// for a directory that contains both .wipnote/ and plugin/.claude-plugin/plugin.json.
// Returns the absolute path to the plugin/ directory, or "" if not found.
// This variant accepts an explicit starting path so dev-mode callers can anchor
// the search to the source repo root rather than CWD (which may be a worktree).
func resolveProjectPluginDirFrom(startDir string) string {
	dir := startDir
	for range 5 {
		// Check if this directory has both .wipnote/ and plugin/
		pluginDir := filepath.Join(dir, "plugin")
		if _, err := os.Stat(filepath.Join(dir, ".wipnote")); err == nil {
			if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err == nil {
				return pluginDir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break // reached filesystem root
		}
		dir = parent
	}
	return ""
}
