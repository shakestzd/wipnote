package main

import (
	"bytes"
	"fmt"
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

// mirrorModsTree makes dst hold the mods a checkout at projectRoot holds:
// its git-tracked files under mods/, without test suites, fixtures and stubs
// (what Claude Code loads needs none of them, and an untracked node_modules or
// generated file never comes along). A checkout with no mods/ (a branch that
// predates them) leaves dst as it is.
//
// It writes a file only when its content or mode differs, and removes only
// what left the source. Every open Claude Code session watches dst and
// reloads a mod whose files change on disk, so the earlier remove-and-recopy
// made each build reload every mod in every session and print a line per mod,
// with nothing changed.
func mirrorModsTree(projectRoot, dst string) error {
	src := filepath.Join(projectRoot, "mods")
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return nil
	}
	out, err := exec.Command("git", "-C", projectRoot, "ls-files", "-z", "--", "mods").Output()
	if err != nil {
		return fmt.Errorf("list tracked mods files: %w", err)
	}
	kept := map[string]bool{}
	for _, rel := range bytes.Split(out, []byte{0}) {
		name := filepath.FromSlash(strings.TrimPrefix(string(rel), "mods/"))
		if name == "" || isModTestFile(name) {
			continue
		}
		copied, err := syncFileMode(filepath.Join(src, name), filepath.Join(dst, name))
		if err != nil {
			return err
		}
		if copied {
			kept[name] = true
		}
	}
	return pruneModsTree(dst, kept)
}

// pruneModsTree removes the files under dst that the mirror did not keep,
// then the folders that left empty. It leaves each kept mod's
// .claude-plugin/types/ alone: Claude Code writes its own type files there
// once it loads the mod, and they are not part of the source.
func pruneModsTree(dst string, kept map[string]bool) error {
	var dirs []string
	err := filepath.WalkDir(dst, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		rel, _ := filepath.Rel(dst, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			// Only for a mod the source still holds. A removed mod must
			// leave no folder: --plugin-dir loads every child of dst.
			if isEngineTypesDir(rel) && kept[filepath.Join(filepath.Dir(rel), "plugin.json")] {
				return filepath.SkipDir
			}
			dirs = append(dirs, path)
			return nil
		}
		if kept[rel] {
			return nil
		}
		if err := os.Remove(path); err != nil {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Deepest first, so a folder is tried after its children. Remove fails on
	// a folder that still holds a file, which is the wanted outcome.
	for i := len(dirs) - 1; i >= 0; i-- {
		os.Remove(dirs[i]) //nolint:errcheck
	}
	return nil
}

// isEngineTypesDir reports whether a path inside the mods tree (relative to
// it) is <mod>/.claude-plugin/types, the folder Claude Code generates.
func isEngineTypesDir(rel string) bool {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return len(parts) == 3 && parts[1] == ".claude-plugin" && parts[2] == "types"
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

// syncFileMode makes dst a copy of src with its mode, creating its folders.
// It leaves dst untouched when content and mode already match, and otherwise
// replaces it by rename, so a watcher never reads a half-written file. It
// reports false when src is gone, with nothing copied.
func syncFileMode(src, dst string) (bool, error) {
	info, err := os.Stat(src)
	if err != nil {
		// Tracked but deleted in the working tree: nothing to copy.
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return false, err
	}
	mode := info.Mode().Perm()
	if have, err := os.Stat(dst); err == nil && have.Mode().IsRegular() && have.Mode().Perm() == mode {
		if old, err := os.ReadFile(dst); err == nil && bytes.Equal(old, data) {
			return true, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".mirror-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return false, err
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return false, err
	}
	return true, nil
}
