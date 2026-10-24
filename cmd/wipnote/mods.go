package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// resolveModsDir returns the folder of Claude Code mods that wipnote loads
// beside its plugin (one child folder per mod, each with its own
// .claude-plugin/plugin.json), or "" when there is none to load.
//
// Claude Code's --plugin-dir loads every child of a folder of plugins, so one
// flag carries all of them. The folder that ships with the plugin tree in use
// comes first (the sibling of pluginDir: in-tree mods/ with --dev, or the
// install the plugin resolved to), so plugin and mods stay one version; when
// that install has none, the shared-tree order applies
// (resolveSharedTreePath: WIPNOTE_MODS_DIR, ~/.local/share/wipnote/mods,
// <brew>/share/wipnote/mods, a source checkout).
//
//   - WIPNOTE_NO_MODS set (and not "0") loads none.
//   - WIPNOTE_MODS_DIR names the folder; one with no mod in it loads none.
func resolveModsDir(pluginDir string) string {
	if off := os.Getenv("WIPNOTE_NO_MODS"); off != "" && off != "0" {
		return ""
	}
	if os.Getenv("WIPNOTE_MODS_DIR") == "" && pluginDir != "" {
		candidate := filepath.Join(filepath.Dir(pluginDir), "mods")
		if abs, err := filepath.Abs(candidate); err == nil {
			candidate = abs
		}
		if isModsTree(candidate) {
			return candidate
		}
	}
	dir, err := resolveSharedTreePath("mods")
	if err != nil {
		debugLog("resolveModsDir: loading no mods: " + err.Error())
		return ""
	}
	// Claude starts in the project root, not here: a relative override must
	// name the same folder there.
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir
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

// modsPromptFile is the per-mod file holding the rule the model needs while
// that mod is loaded (what to write for it to draw).
const modsPromptFile = "prompt.md"

// modsSystemPrompt joins the prompt.md of every mod in modsDir, in folder
// order, for the launcher to append to the system prompt; "" when there is
// none.
//
// The rules ride the launcher's system prompt rather than each mod's
// prompt.compose hook: a managed policy plugin (prepend tier) that sends that
// event past the user tier with next.to skips every mod's hook, and the rule
// silently never reached the model.
func modsSystemPrompt(modsDir string) string {
	if modsDir == "" {
		return ""
	}
	entries, err := os.ReadDir(modsDir)
	if err != nil {
		return ""
	}
	var rules []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(modsDir, entry.Name(), modsPromptFile))
		if err != nil {
			continue
		}
		if rule := strings.TrimSpace(string(data)); rule != "" {
			rules = append(rules, rule)
		}
	}
	return strings.Join(rules, "\n\n")
}

// mirrorModsTree replaces dst with the mods a checkout at projectRoot holds:
// its git-tracked files under mods/, without test suites, fixtures and stubs
// (what Claude Code loads needs none of them, and an untracked node_modules or
// generated file never comes along). A checkout with no mods/ (a branch that
// predates them) leaves dst as it is.
func mirrorModsTree(projectRoot, dst string) error {
	src := filepath.Join(projectRoot, "mods")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	out, err := exec.Command("git", "-C", projectRoot, "ls-files", "-z", "--", "mods").Output()
	if err != nil {
		return fmt.Errorf("list tracked mods files: %w", err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("remove %s: %w", dst, err)
	}
	for _, rel := range bytes.Split(out, []byte{0}) {
		name := filepath.FromSlash(strings.TrimPrefix(string(rel), "mods/"))
		if name == "" || isModTestFile(name) {
			continue
		}
		if err := copyFileMode(filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
			return err
		}
	}
	return nil
}

// isModTestFile reports whether a path inside mods/ (relative to it) is a test
// suite or test support: under a test/ or tests/ folder, or a *.test.* file.
func isModTestFile(rel string) bool {
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		if part == "test" || part == "tests" {
			return true
		}
	}
	return strings.Contains(filepath.Base(rel), ".test.")
}

// copyFileMode copies one file, creating its folders and keeping its mode.
func copyFileMode(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		// Tracked but deleted in the working tree: nothing to copy.
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close() //nolint:errcheck
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close() //nolint:errcheck
		return err
	}
	return out.Close()
}
