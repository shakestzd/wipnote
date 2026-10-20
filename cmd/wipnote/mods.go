package main

import (
	"os"
	"path/filepath"
)

// resolveModsDir returns the folder of Claude Code mods that wipnote loads
// beside its plugin (one child folder per mod, each with its own
// .claude-plugin/plugin.json), or "" when there is none to load.
//
// Claude Code's --plugin-dir loads every child of a folder of plugins, so one
// flag carries all of them. The folder sits beside the plugin tree in every
// layout: ~/.local/share/wipnote/mods, <brew prefix>/share/wipnote/mods, and
// mods/ in a source checkout (--dev).
//
//   - WIPNOTE_NO_MODS set (and not "0") loads none.
//   - WIPNOTE_MODS_DIR names another folder; one with no mod in it loads none.
func resolveModsDir(pluginDir string) string {
	if off := os.Getenv("WIPNOTE_NO_MODS"); off != "" && off != "0" {
		return ""
	}
	if override := os.Getenv("WIPNOTE_MODS_DIR"); override != "" {
		if isModsTree(override) {
			return override
		}
		debugLog("resolveModsDir: WIPNOTE_MODS_DIR=" + override + " holds no mod; loading none")
		return ""
	}
	if pluginDir == "" {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(pluginDir), "mods")
	if abs, err := filepath.Abs(candidate); err == nil {
		candidate = abs
	}
	if isModsTree(candidate) {
		return candidate
	}
	return ""
}

// isModsTree reports whether path is a folder holding at least one mod: a
// child folder with .claude-plugin/plugin.json.
func isModsTree(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, entry.Name(), ".claude-plugin", "plugin.json")); err == nil {
			return true
		}
	}
	return false
}
