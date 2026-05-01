package pluginbuild

import (
	"fmt"
	"os"
	"path/filepath"
)

func init() { Register(codexAdapter{}) }

// codexAdapter emits the Codex CLI marketplace tree. Layout:
//
//	<outDir>/.agents/plugins/marketplace.json
//	<outDir>/.agents/plugins/htmlgraph/.codex-plugin/plugin.json
//	<outDir>/.agents/plugins/htmlgraph/hooks.json
//	<outDir>/.agents/plugins/htmlgraph/.mcp.json
//	<outDir>/.agents/plugins/htmlgraph/{commands,agents,skills,templates,static,config}/
//
// Codex 0.121.0+ registers plugins exclusively via `codex marketplace add <path>`.
// Codex expects the marketplace root to contain `.agents/plugins/marketplace.json`
// and plugin content to live under `.agents/plugins/<plugin-name>/`.
//
// Codex hook event names differ from Claude in a few places (TaskStarted,
// TaskComplete, TurnAborted) — the manifest's `targets` field controls which
// events are emitted here. Business logic stays in `htmlgraph hook <handler>`
// so the Codex plugin is a thin wrapper just like the Claude one.
type codexAdapter struct{}

func (codexAdapter) Name() string { return "codex" }

// codexOwnedSubtrees lists paths relative to the marketplace outDir that
// build-ports fully regenerates. These are cleaned before each emit to prevent
// stale files accumulating. marketplace.json is regenerated separately.
// Hand-maintained files (README.md) outside these paths are never touched.
// The owned subtree is narrowed to the plugin's own directory to avoid
// deleting sibling plugins under .agents/plugins/.
var codexOwnedSubtrees = []string{".agents/plugins/htmlgraph"}

func (c codexAdapter) Emit(m *Manifest, repoRoot, outDir string) error {
	target, ok := m.Targets[c.Name()]
	if !ok {
		return fmt.Errorf("manifest has no target %q", c.Name())
	}

	// Determine where plugin content lives inside the marketplace tree.
	// Codex expects: <outDir>/.agents/plugins/<plugin-name>/
	pluginSubdir := target.PluginSubdir
	if pluginSubdir == "" {
		pluginSubdir = ".agents/plugins/htmlgraph"
	}
	pluginDir := filepath.Join(outDir, pluginSubdir)

	// Pre-clean owned subtrees so renamed/deleted source files don't leave
	// stale output files behind. marketplace.json is inside the owned subtree.
	if err := cleanOwnedSubtrees(outDir, codexOwnedSubtrees); err != nil {
		return fmt.Errorf("codex pre-clean: %w", err)
	}

	// Write marketplace.json at <outDir>/.agents/plugins/marketplace.json.
	// source.path is relative to the directory containing marketplace.json
	// (i.e. .agents/plugins/), so compute as filepath.Rel against that directory.
	mktPath := filepath.Join(outDir, ".agents", "plugins", "marketplace.json")
	mktDir := filepath.Dir(mktPath)
	rel, err := filepath.Rel(mktDir, pluginDir)
	if err != nil {
		return fmt.Errorf("compute relative path for source.path: %w", err)
	}
	sourcePath := "./" + filepath.ToSlash(rel)
	if err := writeCodexMarketplace(m, target, mktPath, sourcePath); err != nil {
		return err
	}

	// Write per-plugin files under plugins/htmlgraph/.
	if err := writeCodexManifest(m, filepath.Join(pluginDir, target.ManifestPath)); err != nil {
		return err
	}
	if err := writeCodexHooks(m, filepath.Join(pluginDir, target.HooksPath)); err != nil {
		return err
	}
	if target.MCPPath != "" {
		if err := ensureCodexMCP(filepath.Join(pluginDir, target.MCPPath)); err != nil {
			return err
		}
	}
	return copyAssets(m, repoRoot, pluginDir)
}

// codexMarketplaceJSON is the schema for marketplace.json at the root of a
// Codex marketplace directory. Codex reads this file on `codex marketplace add`.
type codexMarketplaceJSON struct {
	Name      string                 `json:"name"`
	Interface codexMktInterfaceJSON  `json:"interface"`
	Plugins   []codexMktPluginJSON   `json:"plugins"`
}

type codexMktInterfaceJSON struct {
	DisplayName string `json:"displayName"`
}

type codexMktPluginJSON struct {
	Name     string               `json:"name"`
	Source   codexMktSourceJSON   `json:"source"`
	Policy   codexMktPolicyJSON   `json:"policy"`
	Category string               `json:"category,omitempty"`
}

type codexMktSourceJSON struct {
	Source string `json:"source"`
	Path   string `json:"path"`
}

type codexMktPolicyJSON struct {
	Installation   string `json:"installation"`
	Authentication string `json:"authentication"`
}

// writeCodexMarketplace writes marketplace.json to path. sourcePath is the
// relative path to the plugin directory, computed relative to the directory
// containing marketplace.json (i.e. <outDir>/.agents/plugins/).
func writeCodexMarketplace(m *Manifest, target Target, path, sourcePath string) error {
	name := target.MarketplaceName
	if name == "" {
		name = m.Name
	}
	displayName := target.MarketplaceDisplayName
	if displayName == "" {
		displayName = m.Name
	}
	category := target.MarketplaceCategory
	if category == "" {
		category = m.Category
	}

	// source.path is relative to the directory containing marketplace.json.

	return writeJSON(path, codexMarketplaceJSON{
		Name:      name,
		Interface: codexMktInterfaceJSON{DisplayName: displayName},
		Plugins: []codexMktPluginJSON{
			{
				Name: m.Name,
				Source: codexMktSourceJSON{
					Source: "local",
					Path:   sourcePath,
				},
				Policy: codexMktPolicyJSON{
					Installation:   "AVAILABLE",
					Authentication: "ON_INSTALL",
				},
				Category: category,
			},
		},
	})
}

// codexPluginJSON mirrors the Codex plugin manifest schema. The top-level
// shape is similar to Claude's, plus an `interface` block Codex uses for
// install-surface metadata.
type codexPluginJSON struct {
	Name        string             `json:"name"`
	Version     string             `json:"version"`
	Description string             `json:"description"`
	Author      codexAuthorJSON    `json:"author"`
	Homepage    string             `json:"homepage,omitempty"`
	Repository  string             `json:"repository,omitempty"`
	License     string             `json:"license,omitempty"`
	Keywords    []string           `json:"keywords,omitempty"`
	Skills      string             `json:"skills,omitempty"`
	Interface   codexInterfaceJSON `json:"interface"`
}

type codexAuthorJSON struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
	URL   string `json:"url,omitempty"`
}

type codexInterfaceJSON struct {
	DisplayName      string `json:"displayName"`
	ShortDescription string `json:"shortDescription"`
	LongDescription  string `json:"longDescription,omitempty"`
	DeveloperName    string `json:"developerName"`
	Category         string `json:"category,omitempty"`
}

func writeCodexManifest(m *Manifest, path string) error {
	return writeJSON(path, codexPluginJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author: codexAuthorJSON{
			Name:  m.Author.Name,
			Email: m.Author.Email,
			URL:   m.Author.URL,
		},
		Homepage:   m.Homepage,
		Repository: m.Repository,
		License:    m.License,
		Keywords:   m.Keywords,
		Skills:     "./skills/",
		Interface: codexInterfaceJSON{
			DisplayName:      "HtmlGraph",
			ShortDescription: m.Description,
			DeveloperName:    m.Author.Name,
			Category:         m.Category,
		},
	})
}

// Codex hooks.json schema matches Claude's structure so shared matchers work.
// Different events are supported, not a different schema.
func writeCodexHooks(m *Manifest, path string) error {
	hooks := map[string][]claudeMatcherGroup{}
	order := []string{}

	for _, e := range m.Hooks.Events {
		if !e.AppliesTo("codex") {
			continue
		}
		cmd := e.Command
		if cmd == "" {
			cmd = "htmlgraph hook " + e.Handler
		}
		group := claudeMatcherGroup{
			Matcher: e.Matcher,
			Hooks: []claudeHookEntry{{
				Type:    "command",
				Command: cmd,
				Timeout: e.Timeout,
			}},
		}
		if _, seen := hooks[e.Name]; !seen {
			order = append(order, e.Name)
		}
		hooks[e.Name] = append(hooks[e.Name], group)
	}
	return writeJSON(path, orderedHookMap{keys: order, values: hooks})
}

// ensureCodexMCP writes a stub .mcp.json if none exists. HtmlGraph doesn't
// currently expose an MCP server, but the file is part of the Codex plugin
// contract and future MCP integrations land here without schema churn.
func ensureCodexMCP(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeJSON(path, map[string]any{"mcpServers": map[string]any{}})
}
