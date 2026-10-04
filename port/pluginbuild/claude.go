package pluginbuild

import (
	"fmt"
	"path/filepath"
)

func init() { Register(claudeAdapter{}) }

// claudeAdapter emits the Claude Code plugin tree. Layout:
//
//	<outDir>/.claude-plugin/plugin.json
//	<outDir>/hooks/hooks.json
//	<outDir>/.mcp.json (when target.mcpPath is set)
//	<outDir>/{commands,agents,skills,templates,static,config}/
type claudeAdapter struct{}

func (claudeAdapter) Name() string { return "claude" }

func (c claudeAdapter) Emit(m *Manifest, repoRoot, outDir string) error {
	target, ok := m.Targets[c.Name()]
	if !ok {
		return fmt.Errorf("manifest has no target %q", c.Name())
	}
	if err := writeClaudeManifest(m, filepath.Join(outDir, target.ManifestPath)); err != nil {
		return err
	}
	if err := writeClaudeHooks(m, filepath.Join(outDir, target.HooksPath)); err != nil {
		return err
	}
	if target.MCPPath != "" {
		if err := writeClaudeMCP(filepath.Join(outDir, target.MCPPath)); err != nil {
			return err
		}
	}
	return copyAssets(m, repoRoot, outDir)
}

// claudeMCPServer is one stdio entry in the plugin-root .mcp.json.
type claudeMCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// claudeMCPJSON is the Claude plugin .mcp.json schema:
//
//	{ "mcpServers": { "<name>": { "command": "...", "args": [...] } } }
type claudeMCPJSON struct {
	MCPServers map[string]claudeMCPServer `json:"mcpServers"`
}

// writeClaudeMCP always (re)writes the generated .mcp.json registering the
// read-only `wipnote mcp` stdio server. Like the hooks (`wipnote hook ...`) it
// assumes `wipnote` is on PATH rather than using ${CLAUDE_PLUGIN_ROOT}, because
// the binary is not bundled inside the plugin tree.
func writeClaudeMCP(path string) error {
	return writeJSON(path, claudeMCPJSON{MCPServers: map[string]claudeMCPServer{
		"wipnote": {Command: "wipnote", Args: []string{"mcp"}},
	}})
}

// claudePluginJSON is the Claude-flavored plugin manifest schema.
type claudePluginJSON struct {
	Name        string           `json:"name"`
	Version     string           `json:"version"`
	Description string           `json:"description"`
	Author      claudeAuthorJSON `json:"author"`
	Homepage    string           `json:"homepage,omitempty"`
	Repository  string           `json:"repository,omitempty"`
	License     string           `json:"license,omitempty"`
}

type claudeAuthorJSON struct {
	Name string `json:"name"`
}

func writeClaudeManifest(m *Manifest, path string) error {
	return writeJSON(path, claudePluginJSON{
		Name:        m.Name,
		Version:     m.Version,
		Description: m.Description,
		Author:      claudeAuthorJSON{Name: m.Author.Name},
		Homepage:    m.Homepage,
		Repository:  m.Repository,
		License:     m.License,
	})
}

func translateClaudeAgentFrontmatter(filename string, raw []byte) ([]byte, error) {
	fm, body, hasFM, err := parseAgentFrontmatter(raw)
	if err != nil {
		return nil, err
	}
	if !hasFM {
		return raw, nil
	}
	return renderAgentMarkdown(filterAgentFrontmatter(filename, "claude", fm), body)
}

// Claude hooks.json schema:
//
//	{ "hooks": { "<EventName>": [ { "matcher": "...", "hooks": [ {type, command, timeout?} ] } ] } }
type claudeHookEntry struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Async   bool   `json:"async,omitempty"`
	If      string `json:"if,omitempty"`
}

// hookEntryForTarget builds the emitted hook entry for e. Async and If are
// Claude Code fields; every other target gets only type/command/timeout so its
// (strict) parser never sees a key it does not document.
func hookEntryForTarget(target string, e HookEvent, cmd string) claudeHookEntry {
	entry := claudeHookEntry{Type: "command", Command: cmd, Timeout: e.Timeout}
	if t, ok := e.TimeoutByTarget[target]; ok {
		entry.Timeout = t
	}
	if target == "claude" {
		entry.Async = e.Async
		entry.If = e.If
	}
	return entry
}

type claudeMatcherGroup struct {
	Matcher string            `json:"matcher"`
	Hooks   []claudeHookEntry `json:"hooks"`
}

type claudeHooksJSON struct {
	Hooks map[string][]claudeMatcherGroup `json:"hooks"`
}

func writeClaudeHooks(m *Manifest, path string) error {
	// Preserve declaration order from the manifest: events of the same Name
	// become adjacent matcher groups.
	hooks := map[string][]claudeMatcherGroup{}
	order := []string{}

	for _, e := range m.Hooks.Events {
		if !e.AppliesTo("claude") {
			continue
		}
		cmd := e.Command
		if cmd == "" {
			cmd = "wipnote hook " + e.Handler
		}
		group := claudeMatcherGroup{
			Matcher: e.Matcher,
			Hooks:   []claudeHookEntry{hookEntryForTarget("claude", e, cmd)},
		}
		if _, seen := hooks[e.Name]; !seen {
			order = append(order, e.Name)
		}
		hooks[e.Name] = append(hooks[e.Name], group)
	}

	// Render with stable key order so diffs against a hand-written hooks.json
	// stay minimal. Go encodes maps in key-sorted order already for JSON, but
	// we use a linked struct here to keep event-declaration order.
	orderedOut := orderedHookMap{keys: order, values: hooks}
	return writeJSON(path, orderedOut)
}

// orderedHookMap renders as {"hooks": {...}} with the inner map serialized in
// the exact key order we supply.
type orderedHookMap struct {
	keys   []string
	values map[string][]claudeMatcherGroup
}

func (o orderedHookMap) MarshalJSON() ([]byte, error) {
	var buf []byte
	buf = append(buf, '{')
	buf = append(buf, `"hooks":`...)
	buf = append(buf, '{')
	for i, k := range o.keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, err := jsonMarshal(k)
		if err != nil {
			return nil, err
		}
		buf = append(buf, kb...)
		buf = append(buf, ':')
		vb, err := jsonMarshal(o.values[k])
		if err != nil {
			return nil, err
		}
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	buf = append(buf, '}')
	return buf, nil
}
