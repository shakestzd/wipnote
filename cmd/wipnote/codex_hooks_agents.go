package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type codexHooksFile struct {
	Hooks map[string][]codexHookGroup `json:"hooks"`
}

type codexHookGroup struct {
	Matcher string           `json:"matcher,omitempty"`
	Hooks   []codexHookEntry `json:"hooks"`
}

type codexHookEntry struct {
	Type    string `json:"type,omitempty"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
}

// codexHookHarnessFlag is the suffix the Codex plugin generator appends to
// every `wipnote hook <handler>` command (see port/pluginbuild/codex.go).
const codexHookHarnessFlag = " --harness codex"

func pruneCodexGlobalHooksInstalled(hooksPath, pluginDir string) (bool, error) {
	if pluginDir == "" {
		return false, nil
	}
	commands, err := pluginHookCommands(pluginDir)
	if err != nil {
		return false, err
	}
	if len(commands) == 0 {
		return false, nil
	}

	targetData, err := os.ReadFile(hooksPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading %s: %w", hooksPath, err)
	}
	var target codexHooksFile
	if len(targetData) > 0 {
		if err := json.Unmarshal(targetData, &target); err != nil {
			return false, fmt.Errorf("parsing %s: %w", hooksPath, err)
		}
	}
	if len(target.Hooks) == 0 {
		return false, nil
	}

	if !pruneHookCommands(&target, commands) {
		return false, nil
	}

	out, err := json.MarshalIndent(target, "", "  ")
	if err != nil {
		return false, fmt.Errorf("marshaling %s: %w", hooksPath, err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(hooksPath, out, 0644); err != nil {
		return false, fmt.Errorf("writing %s: %w", hooksPath, err)
	}
	return true, nil
}

// pluginHookCommands returns the hook commands the plugin's hooks.json
// declares, plus their flag-less legacy forms.
func pluginHookCommands(pluginDir string) (map[string]struct{}, error) {
	sourcePath := filepath.Join(pluginDir, "hooks.json")
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("reading plugin hooks %s: %w", sourcePath, err)
	}
	var source codexHooksFile
	if err := json.Unmarshal(sourceData, &source); err != nil {
		return nil, fmt.Errorf("parsing plugin hooks %s: %w", sourcePath, err)
	}
	commands := map[string]struct{}{}
	for _, groups := range source.Hooks {
		for _, group := range groups {
			for _, hook := range group.Hooks {
				cmd := strings.TrimSpace(hook.Command)
				if cmd != "" {
					commands[cmd] = struct{}{}
					// Mirrors written by older wipnote versions predate the
					// --harness flag; match their flag-less form too.
					if legacy := strings.TrimSuffix(cmd, codexHookHarnessFlag); legacy != cmd {
						commands[legacy] = struct{}{}
					}
				}
			}
		}
	}
	return commands, nil
}

// pruneHookCommands removes every hook whose command is in commands from
// target, dropping emptied groups and events, and reports whether anything
// changed.
func pruneHookCommands(target *codexHooksFile, commands map[string]struct{}) bool {
	changed := false
	for eventName, groups := range target.Hooks {
		keptGroups := make([]codexHookGroup, 0, len(groups))
		for _, group := range groups {
			keptHooks := make([]codexHookEntry, 0, len(group.Hooks))
			for _, hook := range group.Hooks {
				if _, remove := commands[strings.TrimSpace(hook.Command)]; remove {
					changed = true
					continue
				}
				keptHooks = append(keptHooks, hook)
			}
			if len(keptHooks) == 0 {
				if len(group.Hooks) > 0 {
					changed = true
				}
				continue
			}
			group.Hooks = keptHooks
			keptGroups = append(keptGroups, group)
		}
		if len(keptGroups) == 0 {
			delete(target.Hooks, eventName)
		} else {
			target.Hooks[eventName] = keptGroups
		}
	}
	return changed
}

func pruneCodexGlobalHooksFromCache() (bool, error) {
	return pruneCodexGlobalHooksInstalled(codexHooksPath(), codexInstalledPluginDirAt(codexPluginCachePath()))
}

func ensureCodexAgentsFromCache() (bool, error) {
	return ensureCodexCustomAgentsInstalled(codexInstalledPluginDirAt(codexPluginCachePath()), codexAgentsPath())
}

// ensureCodexCustomAgentsInstalled mirrors wipnote-*.toml agent definitions from the plugin
// cache into ~/.codex/agents/ and cleans up stale files. Files are intentionally not
// deleted on exit — users running bare codex (outside wipnote) should still see the agents.
// Stale wipnote-*.toml files from old versions are cleaned up here on the next launch.
func ensureCodexCustomAgentsInstalled(pluginDir, agentsDir string) (bool, error) {
	if pluginDir == "" || agentsDir == "" {
		return false, nil
	}
	parentDir := filepath.Dir(agentsDir)
	if info, err := os.Stat(parentDir); err == nil && !info.IsDir() {
		return false, nil
	} else if err != nil && !os.IsNotExist(err) {
		return false, fmt.Errorf("checking Codex agents parent %s: %w", parentDir, err)
	}
	sourceDir := filepath.Join(pluginDir, "agents")
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading Codex agent source %s: %w", sourceDir, err)
	}

	changed := false
	sourceNames := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".toml") {
			continue
		}
		sourceNames[entry.Name()] = true
		sourcePath := filepath.Join(sourceDir, entry.Name())
		targetPath := filepath.Join(agentsDir, entry.Name())
		if sameFileContent(sourcePath, targetPath) {
			continue
		}
		if err := os.MkdirAll(agentsDir, 0755); err != nil {
			return false, fmt.Errorf("creating Codex agents dir %s: %w", agentsDir, err)
		}
		if err := copyFile(sourcePath, targetPath); err != nil {
			return false, fmt.Errorf("installing Codex agent %s: %w", targetPath, err)
		}
		changed = true
	}
	removed, err := removeStaleCodexAgents(agentsDir, sourceNames)
	if err != nil {
		return false, err
	}
	return changed || removed, nil
}

// removeStaleCodexAgents removes wipnote-*.toml agents from agentsDir that the
// plugin no longer ships. Non-wipnote files are never touched.
func removeStaleCodexAgents(agentsDir string, sourceNames map[string]bool) (bool, error) {
	targetEntries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading Codex agents target %s: %w", agentsDir, err)
	}
	changed := false
	for _, entry := range targetEntries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "wipnote-") || !strings.HasSuffix(name, ".toml") || sourceNames[name] {
			continue
		}
		if err := os.Remove(filepath.Join(agentsDir, name)); err != nil {
			return false, fmt.Errorf("removing stale Codex agent %s: %w", name, err)
		}
		changed = true
	}
	return changed, nil
}

func sameFileContent(a, b string) bool {
	left, err := os.ReadFile(a)
	if err != nil {
		return false
	}
	right, err := os.ReadFile(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// removeCodexProjectAgentCopies deletes wipnote-*.toml files that older
// wipnote versions copied into <project>/.codex/agents. wipnote now
// registers its agents only once, in ~/.codex/agents: Codex discovers roles
// from every config layer's `agents/` folder
// (codex-rs/agent-roles/src/loader.rs:75-79), so the user-level copy is
// already visible in every project. A leftover project copy is worse than
// redundant — the project layer sits above the user layer and its role
// replaces the user one (loader.rs:100-112), so a stale project file would
// shadow the freshly-synced user-level agent. Only wipnote-*.toml files are
// touched (wipnote overwrote them on every launch, so they hold no user
// edits); anything else in the directory is left alone.
func removeCodexProjectAgentCopies(projectRoot string) (bool, error) {
	agentsDir := codexProjectAgentsPath(projectRoot)
	if agentsDir == "" {
		return false, nil
	}
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading project Codex agents %s: %w", agentsDir, err)
	}
	changed := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "wipnote-") || !strings.HasSuffix(name, ".toml") {
			continue
		}
		if err := os.Remove(filepath.Join(agentsDir, name)); err != nil {
			return changed, fmt.Errorf("removing project Codex agent copy %s: %w", name, err)
		}
		changed = true
	}
	return changed, nil
}

func copyDir(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", src)
	}
	if err := os.MkdirAll(dst, info.Mode().Perm()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(srcPath, dstPath); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
