package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMarketplaceWipnotePresent_FalseWhenNothingInstalled verifies that
// marketplaceWipnotePresent returns false when HOME contains no plugin files.
// This guards the Fix 1 fast-path: removeMarketplaceWipnote must skip all
// subprocess calls (~4s) when the marketplace plugin was never installed.
func TestMarketplaceWipnotePresent_FalseWhenNothingInstalled(t *testing.T) {
	// Point HOME at an empty temp dir so os.UserHomeDir() finds no plugin files.
	t.Setenv("HOME", t.TempDir())

	got := marketplaceWipnotePresent()
	if got {
		t.Error("marketplaceWipnotePresent() = true, want false when no plugin files exist")
	}
}

// TestMarketplaceWipnotePresent_TrueWhenMarketplaceDirExists verifies that
// marketplaceWipnotePresent returns true when the marketplaces/wipnote dir is present,
// even when installed_plugins.json has no entry.
func TestMarketplaceWipnotePresent_TrueWhenMarketplaceDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create the marketplace dir that indicates a cloned plugin.
	mktDir := filepath.Join(home, ".claude", "plugins", "marketplaces", "wipnote")
	if err := os.MkdirAll(mktDir, 0755); err != nil {
		t.Fatalf("mkdir marketplaces/wipnote: %v", err)
	}

	got := marketplaceWipnotePresent()
	if !got {
		t.Error("marketplaceWipnotePresent() = false, want true when marketplaces/wipnote dir exists")
	}
}

// TestMarketplaceWipnotePresent_TrueWhenPluginInstalled verifies that
// marketplaceWipnotePresent returns true when installed_plugins.json records the plugin.
func TestMarketplaceWipnotePresent_TrueWhenPluginInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write a valid installed_plugins.json with wipnote entry.
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	data := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"wipnote@wipnote": []map[string]string{
				{"scope": "wipnote", "installPath": "/some/path", "version": "0.39.0"},
			},
		},
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), b, 0644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}

	got := marketplaceWipnotePresent()
	if !got {
		t.Error("marketplaceWipnotePresent() = false, want true when plugin entry exists in installed_plugins.json")
	}
}

// TestMarketplaceWipnotePresent_TrueWhenLocalMarketplaceDirExists verifies that
// marketplaceWipnotePresent returns true when the cache/local-marketplace/wipnote dir is present.
// This detects a local-marketplace-only install that would otherwise be missed.
func TestMarketplaceWipnotePresent_TrueWhenLocalMarketplaceDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create the local-marketplace dir that indicates a cached local plugin.
	mktDir := filepath.Join(home, ".claude", "plugins", "cache", "local-marketplace", "wipnote")
	if err := os.MkdirAll(mktDir, 0755); err != nil {
		t.Fatalf("mkdir cache/local-marketplace/wipnote: %v", err)
	}

	got := marketplaceWipnotePresent()
	if !got {
		t.Error("marketplaceWipnotePresent() = false, want true when cache/local-marketplace/wipnote dir exists")
	}
}

// TestMarketplaceWipnotePresent_TrueWhenHtmlgraphDirExists verifies that
// marketplaceWipnotePresent returns true when legacy htmlgraph artifact dirs exist.
// This detects old htmlgraph installs that need cleanup.
func TestMarketplaceWipnotePresent_TrueWhenHtmlgraphDirExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Create a legacy htmlgraph cache dir.
	mktDir := filepath.Join(home, ".claude", "plugins", "cache", "htmlgraph")
	if err := os.MkdirAll(mktDir, 0755); err != nil {
		t.Fatalf("mkdir cache/htmlgraph: %v", err)
	}

	got := marketplaceWipnotePresent()
	if !got {
		t.Error("marketplaceWipnotePresent() = false, want true when cache/htmlgraph dir exists")
	}
}

// TestIsAnyMarketplacePluginInstalledAt_LocalMarketplaceScope verifies that
// isAnyMarketplacePluginInstalledAt detects a wipnote@local-marketplace entry,
// not only wipnote@wipnote.
func TestIsAnyMarketplacePluginInstalledAt_LocalMarketplaceScope(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsFile := filepath.Join(tmpDir, "installed_plugins.json")

	data := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"wipnote@local-marketplace": []map[string]string{
				{"scope": "local-marketplace", "installPath": "/some/path", "version": "0.63.0"},
			},
		},
	}
	b, _ := json.Marshal(data)
	os.WriteFile(pluginsFile, b, 0644)

	// isAnyMarketplacePluginInstalledAt must detect the local-marketplace scope.
	if !isAnyMarketplacePluginInstalledAt(pluginsFile) {
		t.Error("isAnyMarketplacePluginInstalledAt() = false for wipnote@local-marketplace, want true")
	}
}

// TestIsAnyMarketplacePluginInstalledAt_HtmlgraphScope verifies that
// isAnyMarketplacePluginInstalledAt detects legacy htmlgraph@htmlgraph registry entries.
func TestIsAnyMarketplacePluginInstalledAt_HtmlgraphScope(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsFile := filepath.Join(tmpDir, "installed_plugins.json")

	data := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"htmlgraph@htmlgraph": []map[string]string{
				{"scope": "htmlgraph", "installPath": "/old/path", "version": "0.30.0"},
			},
		},
	}
	b, _ := json.Marshal(data)
	os.WriteFile(pluginsFile, b, 0644)

	if !isAnyMarketplacePluginInstalledAt(pluginsFile) {
		t.Error("isAnyMarketplacePluginInstalledAt() = false for htmlgraph@htmlgraph, want true")
	}
}

// TestIsAnyMarketplacePluginInstalledAt_HtmlgraphLocalMarketplaceScope verifies
// that isAnyMarketplacePluginInstalledAt detects legacy htmlgraph@local-marketplace entries.
func TestIsAnyMarketplacePluginInstalledAt_HtmlgraphLocalMarketplaceScope(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsFile := filepath.Join(tmpDir, "installed_plugins.json")

	data := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"htmlgraph@local-marketplace": []map[string]string{
				{"scope": "local-marketplace", "installPath": "/old/path", "version": "0.30.0"},
			},
		},
	}
	b, _ := json.Marshal(data)
	os.WriteFile(pluginsFile, b, 0644)

	if !isAnyMarketplacePluginInstalledAt(pluginsFile) {
		t.Error("isAnyMarketplacePluginInstalledAt() = false for htmlgraph@local-marketplace, want true")
	}
}

// TestIsAnyMarketplacePluginInstalledAt_FalseWhenEmpty verifies that
// isAnyMarketplacePluginInstalledAt returns false when no known scopes are present.
func TestIsAnyMarketplacePluginInstalledAt_FalseWhenEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	pluginsFile := filepath.Join(tmpDir, "installed_plugins.json")

	data := map[string]any{"version": 1, "plugins": map[string]any{}}
	b, _ := json.Marshal(data)
	os.WriteFile(pluginsFile, b, 0644)

	if isAnyMarketplacePluginInstalledAt(pluginsFile) {
		t.Error("isAnyMarketplacePluginInstalledAt() = true for empty plugins, want false")
	}
}

// TestMarketplaceWipnotePresent_TrueWhenLocalMarketplaceRegistryOnly verifies
// that marketplaceWipnotePresent returns true when installed_plugins.json
// contains only a wipnote@local-marketplace entry and no artifact dirs exist.
// This is the bug that the Finding 4 fix addresses: registry-only detection
// now covers all four removal scopes, not just wipnote@wipnote.
func TestMarketplaceWipnotePresent_TrueWhenLocalMarketplaceRegistryOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Write installed_plugins.json with only wipnote@local-marketplace.
	pluginsDir := filepath.Join(home, ".claude", "plugins")
	if err := os.MkdirAll(pluginsDir, 0755); err != nil {
		t.Fatalf("mkdir plugins: %v", err)
	}
	data := map[string]any{
		"version": 1,
		"plugins": map[string]any{
			"wipnote@local-marketplace": []map[string]string{
				{"scope": "local-marketplace", "installPath": "/some/path", "version": "0.63.0"},
			},
		},
	}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(filepath.Join(pluginsDir, "installed_plugins.json"), b, 0644); err != nil {
		t.Fatalf("write installed_plugins.json: %v", err)
	}
	// No artifact dirs — registry entry alone must trigger true.

	got := marketplaceWipnotePresent()
	if !got {
		t.Error("marketplaceWipnotePresent() = false, want true for registry-only wipnote@local-marketplace (no artifact dirs)")
	}
}
