package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Where Claude Code records plugins (code.claude.com/docs/en/plugins/loading,
// checked 2026-10-28): files under the plugins root (~/.claude/plugins, or
// CLAUDE_CODE_PLUGIN_CACHE_DIR), installs in installed_plugins.json
// ({"plugins": {"<name>@<marketplace>": [{"installPath": ...}]}}), the
// on/off switch in enabledPlugins ("<name>@<origin>": true|false), and
// plugins synced from claude.ai under synced/ as "<name>@synced", on unless
// set to false.

// pluginsRoot is Claude Code's plugins root for home.
func pluginsRoot(p projectThreadProbe) string {
	if dir := p.Getenv("CLAUDE_CODE_PLUGIN_CACHE_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(p.Home, ".claude", "plugins")
}

// enabledWipnotePlugin returns the directory of a wipnote plugin Claude Code
// installed and enabled, or synced and left on, else "". A marketplace
// clone, or a cached copy with no enabled install, does not count.
// Safest default: only the user settings file is read; a thread does not
// read the repository's .claude/settings.json, and managed settings are
// not checked.
func enabledWipnotePlugin(p projectThreadProbe) string {
	root := pluginsRoot(p)
	enabled := readEnabledPlugins(filepath.Join(p.Home, ".claude", "settings.json"))
	for id, dirs := range readInstalledPlugins(filepath.Join(root, "installed_plugins.json")) {
		if !strings.HasPrefix(id, "wipnote@") || enabled[id] != "true" {
			continue
		}
		for _, dir := range dirs {
			if isWipnotePluginDir(dir) {
				return dir
			}
		}
	}
	if enabled["wipnote@synced"] == "false" {
		return ""
	}
	return findWipnotePlugin(filepath.Join(root, "synced"))
}

// readInstalledPlugins maps each plugin id to its install paths.
func readInstalledPlugins(path string) map[string][]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var f struct {
		Plugins map[string][]struct {
			InstallPath string `json:"installPath"`
		} `json:"plugins"`
	}
	if json.Unmarshal(b, &f) != nil {
		return nil
	}
	out := map[string][]string{}
	for id, entries := range f.Plugins {
		for _, e := range entries {
			if e.InstallPath != "" {
				out[id] = append(out[id], e.InstallPath)
			}
		}
	}
	return out
}

// readEnabledPlugins maps each enabledPlugins id to "true" or "false";
// any other value is left out.
func readEnabledPlugins(path string) map[string]string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var s struct {
		EnabledPlugins map[string]json.RawMessage `json:"enabledPlugins"`
	}
	if json.Unmarshal(b, &s) != nil {
		return nil
	}
	out := map[string]string{}
	for id, raw := range s.EnabledPlugins {
		if v := strings.TrimSpace(string(raw)); v == "true" || v == "false" {
			out[id] = v
		}
	}
	return out
}
