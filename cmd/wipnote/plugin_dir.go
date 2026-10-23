package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// resolvePluginDir finds the go-plugin directory using a priority-ordered
// search strategy. This decouples plugin discovery from the binary's
// filesystem location, supporting Homebrew, go install, curl install, and
// dev-mode symlink workflows.
//
// Search order:
//  1. CLAUDE_PLUGIN_ROOT env var (always set by Claude Code in hook/plugin context)
//  2. WIPNOTE_PLUGIN_DIR env var (explicit user override)
//  3. installed_plugins.json installPath (marketplace: ~/.claude/plugins/cache/...)
//  4. Symlink walk-up from binary (dev mode: binary lives inside plugin tree)
//  5. Project-root detection (CWD walk-up: find .wipnote/ + plugin/)
func resolvePluginDir() string {
	// 1. CLAUDE_PLUGIN_ROOT — set by Claude Code whenever a hook runs.
	//    This is the authoritative source in hook and plugin context, and
	//    works correctly for both dev-mode symlinks and marketplace installs.
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		if _, err := os.Stat(filepath.Join(root, ".claude-plugin", "plugin.json")); err == nil {
			return root
		}
	}

	// 2. Explicit user override — useful for non-standard installs or testing.
	if dir := os.Getenv("WIPNOTE_PLUGIN_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err == nil {
			return dir
		}
	}

	// 3. Read installed_plugins.json to find the marketplace install path.
	//    Claude Code stores plugins at ~/.claude/plugins/cache/<marketplace>/<name>/<version>/
	//    and records the exact path in installed_plugins.json.  Iterating the
	//    file is more robust than hard-coding a path that varies by version.
	if dir := resolveMarketplacePluginDir(); dir != "" {
		return dir
	}

	// 4. Symlink walk-up from binary — works for dev mode where the binary
	//    lives at plugin/hooks/bin/wipnote (two levels up is
	//    the plugin root).  Fails gracefully when the binary is at
	//    ~/.local/bin/wipnote (standalone) or inside the marketplace cache
	//    (already handled above), because those paths have no plugin.json.
	binPath, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(binPath); err == nil {
		binPath = resolved
	}
	pluginDir := filepath.Join(filepath.Dir(binPath), "..", "..")
	pluginDir, _ = filepath.Abs(pluginDir)
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); err == nil {
		return pluginDir
	}

	// 5. Project-root detection — walk up from CWD to find .wipnote/,
	//    then check for plugin/ relative to the project root.
	//    This makes dev mode work from a fresh clone or fork without
	//    needing a marketplace install first.
	if projectPlugin := resolveProjectPluginDir(); projectPlugin != "" {
		return projectPlugin
	}

	return ""
}

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

// resolveMarketplacePluginDir reads ~/.claude/plugins/installed_plugins.json
// and returns the first installPath that has a valid .claude-plugin/plugin.json
// and whose key contains "wipnote". Returns "" on any error or miss.
func resolveMarketplacePluginDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "plugins", "installed_plugins.json"))
	if err != nil {
		return ""
	}

	var registry struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &registry); err != nil {
		return ""
	}

	for key, entries := range registry.Plugins {
		if !strings.Contains(key, "wipnote") {
			continue
		}
		for _, e := range entries {
			if e.InstallPath == "" {
				continue
			}
			candidate := e.InstallPath
			// Resolve symlinks (dev-mode swap replaces the cache dir with a
			// symlink to the source tree — we want the real plugin root).
			if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
				candidate = resolved
			}
			if _, err := os.Stat(filepath.Join(candidate, ".claude-plugin", "plugin.json")); err == nil {
				return candidate
			}
		}
	}
	return ""
}
