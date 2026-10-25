package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// codexPluginCachePath returns Codex's cache location for the wipnote plugin.
func codexPluginCachePath() string {
	return filepath.Join(codexHomeDir(), "plugins", "cache", "wipnote", "wipnote")
}

// isCodexMarketplaceInstalledAt is the testable core that reads the given path.
func isCodexMarketplaceInstalledAt(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "[marketplaces.wipnote]") ||
		strings.Contains(content, `[plugins."wipnote@wipnote"]`)
}

// isCodexHooksExplicitlyDisabledAt reports whether the user's config.toml
// turns Codex hooks OFF. wipnote no longer writes `[features] hooks = true`:
// the hooks feature is Stage::Stable with default_enabled: true
// (codex-rs/features/src/lib.rs:1357-1362), so an absent key already means
// "on", and rewriting the user's config on every launch was needless churn.
// The only case left to handle is a deliberate opt-out, which we respect and
// merely warn about (see warnIfCodexHooksDisabled).
//
// Resolution mirrors Codex: `codex_hooks` is a deprecated alias of the same
// feature (codex-rs/features/src/legacy.rs:48-51) and Features::apply_map
// walks a BTreeMap (codex-rs/features/src/lib.rs:644-645), so "codex_hooks"
// is applied before "hooks" and the canonical key wins when both are set.
func isCodexHooksExplicitlyDisabledAt(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	tree := make(map[string]any)
	if err := toml.Unmarshal(data, &tree); err != nil {
		return false
	}

	features, ok := tree["features"].(map[string]any)
	if !ok {
		return false
	}
	if enabled, ok := features["hooks"].(bool); ok {
		return !enabled
	}
	if enabled, ok := features["codex_hooks"].(bool); ok {
		return !enabled
	}
	return false
}

// warnIfCodexHooksDisabled prints a stderr warning (never rewrites the
// config) when the user has explicitly disabled Codex hooks, since every
// wipnote Codex integration — attribution, guards, session tracking — rides
// on hooks and would otherwise fail silently.
func warnIfCodexHooksDisabled(configPath string) {
	if isCodexHooksExplicitlyDisabledAt(configPath) {
		fmt.Fprintf(os.Stderr, "wipnote: warning: Codex hooks are disabled in %s ([features] hooks = false); wipnote hooks will not run. Remove that line or set it to true to enable them.\n", configPath)
	}
}

// isCodexPluginEnabledAt returns true when the wipnote plugin itself is enabled.
// Marketplace registration only makes the plugin available; Codex loads skills
// and commands from enabled plugins.
func isCodexPluginEnabledAt(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}

	tree := make(map[string]any)
	if err := toml.Unmarshal(data, &tree); err != nil {
		return false
	}

	if plugins, ok := tree["plugins"].(map[string]any); ok {
		if plugin, ok := plugins[codexPluginID].(map[string]any); ok {
			if enabled, ok := plugin["enabled"].(bool); ok {
				return enabled
			}
		}
	}
	return false
}

// isCodexPluginInstalledAt returns true when Codex has a loadable plugin tree
// in its plugin cache. Codex expects one install-version/hash directory below
// ~/.codex/plugins/cache/<marketplace>/<plugin>.
func isCodexPluginInstalledAt(cachePath string) bool {
	return codexInstalledPluginDirAt(cachePath) != ""
}

func codexInstalledPluginDirAt(cachePath string) string {
	entries, err := os.ReadDir(cachePath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if _, err := os.Stat(filepath.Join(cachePath, entry.Name(), ".codex-plugin", "plugin.json")); err == nil {
			return filepath.Join(cachePath, entry.Name())
		}
	}
	return ""
}

// codexManifestPath resolves the path to marketplace.json inside a
// codex-marketplace tree, transparently handling both the flat layout
// produced by the legacy GoReleaser archive (marketplace.json at the tree
// root) and the deep layout used by the dev source and `wipnote build`
// install (.agents/plugins/marketplace.json).
//
// This is used to LOCATE and parse the manifest (e.g. to derive the local
// plugin directory). It must NOT be passed to `codex plugin marketplace add`,
// which requires a marketplace ROOT DIRECTORY — see codexMarketplaceAddArg.
func codexManifestPath(treeRoot string) string {
	flat := filepath.Join(treeRoot, "marketplace.json")
	if _, err := os.Stat(flat); err == nil {
		return flat
	}
	return filepath.Join(treeRoot, ".agents", "plugins", "marketplace.json")
}

// codexMarketplaceAddArg resolves the directory to pass to
// `codex plugin marketplace add`. Codex CLI requires a local marketplace
// ROOT DIRECTORY whose layout is `<root>/.agents/plugins/marketplace.json`;
// passing the marketplace.json file directly fails with
// "local marketplace source must be a directory, not a file".
//
// The dev source, `wipnote build` install, and (after the v0.60.10 archive
// fix) the release tarball all keep the manifest nested under
// `<root>/.agents/plugins/marketplace.json`, so the bundled tree root IS the
// directory to register. For backward compatibility with an older flattened
// tarball that placed marketplace.json directly at the tree root, we fall
// back to the tree root itself (which is still a directory, satisfying
// Codex's directory requirement).
func codexMarketplaceAddArg(treeRoot string) string {
	nested := filepath.Join(treeRoot, ".agents", "plugins", "marketplace.json")
	if _, err := os.Stat(nested); err == nil {
		return treeRoot
	}
	// Legacy flat layout: marketplace.json at the tree root. The directory is
	// still treeRoot; Codex accepts the directory and resolves the manifest.
	return treeRoot
}

// getCodexMarketplacePathAt parses config.toml and returns the registered wipnote
// marketplace path, or empty string if not found.
func getCodexMarketplacePathAt(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}

	tree := make(map[string]any)
	if err := toml.Unmarshal(data, &tree); err != nil {
		return ""
	}

	// Check [marketplaces.wipnote]
	if mkts, ok := tree["marketplaces"].(map[string]any); ok {
		if hg, ok := mkts["wipnote"].(map[string]any); ok {
			if source, ok := hg["source"].(string); ok {
				return source
			}
			if path, ok := hg["path"].(string); ok {
				return path
			}
		}
	}

	// Check [plugins."wipnote@wipnote"]
	if plugins, ok := tree["plugins"].(map[string]any); ok {
		if hg, ok := plugins["wipnote@wipnote"].(map[string]any); ok {
			if source, ok := hg["source"].(string); ok {
				return source
			}
			if path, ok := hg["path"].(string); ok {
				return path
			}
		}
	}

	return ""
}

// removeCodexWipnoteRegistrations removes any wipnote marketplace or plugin
// registrations from the given config.toml file. It is idempotent — if the file
// does not exist or contains no wipnote entries, it is a no-op.
// Returns (removed bool, error). removed=true indicates at least one entry was deleted.
func removeCodexWipnoteRegistrations(configPath string) (bool, error) {
	// Read existing config, if any
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil // file doesn't exist; no-op
		}
		return false, fmt.Errorf("reading %s: %w", configPath, err)
	}

	// Parse the TOML tree
	tree := make(map[string]any)
	if len(data) > 0 {
		if err := toml.Unmarshal(data, &tree); err != nil {
			return false, fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	removed := false

	// Remove from [plugins]. The htmlgraph key is a legacy registration that
	// must be cleaned up so it cannot shadow the renamed wipnote plugin.
	if removeTOMLSectionKeys(tree, "plugins", "wipnote@wipnote", "htmlgraph@htmlgraph") {
		removed = true
	}

	// Remove from [marketplaces]. Keep removing the legacy htmlgraph entry for
	// users who installed the plugin before the rename.
	if removeTOMLSectionKeys(tree, "marketplaces", "wipnote", "htmlgraph") {
		removed = true
	}

	// If nothing was removed, no need to rewrite the file
	if !removed {
		return false, nil
	}

	// Marshal back to TOML and write
	newData, err := toml.Marshal(tree)
	if err != nil {
		return false, fmt.Errorf("marshaling TOML: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return false, fmt.Errorf("writing %s: %w", configPath, err)
	}

	return true, nil
}

// removeTOMLSectionKeys deletes keys from tree[section] and reports whether
// any existed. If the section is now empty, the whole section is removed.
func removeTOMLSectionKeys(tree map[string]any, section string, keys ...string) bool {
	removed := false
	if sec, ok := tree[section].(map[string]any); ok {
		for _, key := range keys {
			if _, exists := sec[key]; exists {
				delete(sec, key)
				removed = true
			}
		}
		if len(sec) == 0 {
			delete(tree, section)
		}
	}
	return removed
}

// ensureCodexPluginEnabled enables the installed marketplace plugin so Codex
// loads its skills, commands, hooks, and other plugin assets.
func ensureCodexPluginEnabled(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", configPath, err)
	}

	tree := make(map[string]any)
	if err == nil && len(data) > 0 {
		if err := toml.Unmarshal(data, &tree); err != nil {
			return fmt.Errorf("parsing %s: %w", configPath, err)
		}
	}

	plugins, ok := tree["plugins"].(map[string]any)
	if !ok {
		plugins = make(map[string]any)
		tree["plugins"] = plugins
	}
	plugin, ok := plugins[codexPluginID].(map[string]any)
	if !ok {
		plugin = make(map[string]any)
		plugins[codexPluginID] = plugin
	}
	plugin["enabled"] = true

	newData, err := toml.Marshal(tree)
	if err != nil {
		return fmt.Errorf("marshaling TOML: %w", err)
	}

	if err := os.WriteFile(configPath, newData, 0644); err != nil {
		return fmt.Errorf("writing %s: %w", configPath, err)
	}

	return nil
}

// ensureCodexLocalPluginInstalled materializes a local marketplace plugin into
// Codex's plugin cache. Codex currently loads enabled plugins from
// ~/.codex/plugins/cache/<marketplace>/<plugin>; registering a local marketplace
// alone leaves the enabled plugin with no installed tree to load.
func ensureCodexLocalPluginInstalled(configPath string, force bool) (bool, error) {
	if !force && isCodexPluginInstalledAt(codexPluginCachePath()) {
		return false, nil
	}

	marketplacePath := getCodexMarketplacePathAt(configPath)
	if marketplacePath == "" {
		return false, nil
	}

	// Support both: legacy directory-root registration (manifest under
	// .agents/plugins/marketplace.json) and new manifest-file registration.
	// Either way we need the marketplace ROOT (not just the manifest path) —
	// see codexPluginDirFromMarketplace for why.
	mktPath := marketplacePath
	treeRoot := marketplacePath
	if info, err := os.Stat(mktPath); err == nil && info.IsDir() {
		mktPath = codexManifestPath(marketplacePath)
	} else {
		treeRoot = codexTreeRootFromManifestPath(mktPath)
	}
	pluginDir, err := codexPluginDirFromMarketplace(mktPath, treeRoot)
	if err != nil {
		return false, nil
	}
	if err := installCodexPluginCache(pluginDir, codexPluginCachePath()); err != nil {
		return false, err
	}
	return true, nil
}

// codexTreeRootFromManifestPath inverts codexManifestPath: given a
// marketplace.json path, it returns the marketplace tree root. Handles both
// the nested layout (<root>/.agents/plugins/marketplace.json) and the legacy
// flat layout (<root>/marketplace.json).
func codexTreeRootFromManifestPath(manifestPath string) string {
	dir := filepath.Dir(manifestPath)
	if filepath.Base(dir) == "plugins" && filepath.Base(filepath.Dir(dir)) == ".agents" {
		return filepath.Dir(filepath.Dir(dir))
	}
	return dir
}

// codexPluginDirFromMarketplace resolves a marketplace.json's declared
// plugin source.path to an actual directory on disk. treeRoot is the
// marketplace ROOT directory (the directory registered under
// [marketplaces.<name>], NOT the directory containing marketplace.json).
//
// source.path is resolved relative to treeRoot, matching Codex's own
// resolution — verified live against codex-cli 0.147.0 (bug-040f0be8):
// `codex plugin add` joins the declared path onto the registered marketplace
// root, not onto the manifest's own directory. Resolving relative to
// filepath.Dir(marketplaceJSONPath) instead (as this function used to)
// silently disagreed with Codex whenever the manifest is nested (e.g. under
// .agents/plugins/), and must be kept in agreement with the codex adapter's
// Emit function in port/pluginbuild/codex.go.
func codexPluginDirFromMarketplace(marketplaceJSONPath, treeRoot string) (string, error) {
	data, err := os.ReadFile(marketplaceJSONPath)
	if err != nil {
		return "", err
	}
	var marketplace struct {
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				Path   string `json:"path"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &marketplace); err != nil {
		return "", err
	}
	for _, plugin := range marketplace.Plugins {
		if plugin.Name != "wipnote" || plugin.Source.Source != "local" || plugin.Source.Path == "" {
			continue
		}
		pluginDir := filepath.Clean(filepath.Join(treeRoot, plugin.Source.Path))
		if _, err := os.Stat(filepath.Join(pluginDir, ".codex-plugin", "plugin.json")); err == nil {
			return pluginDir, nil
		}
	}
	return "", os.ErrNotExist
}

func installCodexPluginCache(pluginDir, cachePath string) error {
	if err := os.RemoveAll(cachePath); err != nil {
		return err
	}
	if err := os.MkdirAll(cachePath, 0755); err != nil {
		return err
	}
	installPath := filepath.Join(cachePath, codexLocalPluginCacheVersion)
	return copyDir(pluginDir, installPath)
}
