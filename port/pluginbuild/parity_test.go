package pluginbuild

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestClaudeParityFromLiveManifest guards the Claude plugin port against
// regressions in the shared manifest. It loads the real
// packages/plugin-core/manifest.json, emits a Claude plugin tree into a
// tempdir, and asserts the invariants the wipnote plugin must satisfy:
// manifest name/version, the three workhorse hook events, and at least one
// command/agent/skill copied from the asset sources. The test is
// self-contained: it does not shell out, hit the network, or depend on the
// wipnote binary being installed.
func TestClaudeParityFromLiveManifest(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	manifestPath, err := FindManifest(cwd)
	if err != nil {
		t.Fatalf("FindManifest: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath))) // .../packages/plugin-core/manifest.json → repo root

	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	outDir := t.TempDir()
	if err := (claudeAdapter{}).Emit(m, repoRoot, outDir); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	// manifest name/version
	var plug claudePluginJSON
	manifestBytes, err := os.ReadFile(filepath.Join(outDir, ".claude-plugin", "plugin.json"))
	if err != nil {
		t.Fatalf("read plugin.json: %v", err)
	}
	if err := json.Unmarshal(manifestBytes, &plug); err != nil {
		t.Fatalf("unmarshal plugin.json: %v", err)
	}
	if plug.Name != m.Name {
		t.Errorf("plugin.json name=%q want %q", plug.Name, m.Name)
	}
	if plug.Version != m.Version {
		t.Errorf("plugin.json version=%q want %q", plug.Version, m.Version)
	}

	// hooks.json carries the three workhorse events every Claude session uses.
	hooksBytes, err := os.ReadFile(filepath.Join(outDir, "hooks", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	hooks := string(hooksBytes)
	for _, want := range []string{`"SessionStart"`, `"PreToolUse"`, `"PostToolUse"`} {
		if !strings.Contains(hooks, want) {
			t.Errorf("hooks.json missing %s", want)
		}
	}

	// Events belonging to another harness's vocabulary must not leak into the
	// Claude output. AfterAgent is antigravity-only canonical vocabulary;
	// PreInvocation/PostInvocation are agy-native names produced by the
	// antigravity translation layer and are meaningless to Claude Code.
	for _, notWant := range []string{`"AfterAgent"`, `"PreInvocation"`, `"PostInvocation"`} {
		if strings.Contains(hooks, notWant) {
			t.Errorf("hooks.json contains non-Claude event %s", notWant)
		}
	}

	// At least one agent copied over.
	assertHasMarkdown(t, filepath.Join(outDir, "agents"), "agents")
}

func assertHasMarkdown(t *testing.T, dir, label string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", label, err)
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			// Guard against the same-path bug that silently truncated assets: any
			// .md file must be non-empty to count as successfully copied.
			info, err := e.Info()
			if err != nil {
				t.Fatalf("stat %s/%s: %v", label, e.Name(), err)
			}
			if info.Size() > 0 {
				return
			}
		}
	}
	t.Errorf("no non-empty .md under %s", dir)
}

// TestLiveGeneratedPortsCarryNoSkillsOrCommands checks that the Codex and
// Antigravity trees hold no skills or commands now that the source folders are
// gone (feat-9796e97f).
func TestLiveGeneratedPortsCarryNoSkillsOrCommands(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	outBase := t.TempDir()
	codexOut := filepath.Join(outBase, "codex")
	if err := (codexAdapter{}).Emit(m, repoRoot, codexOut); err != nil {
		t.Fatalf("emit codex: %v", err)
	}
	antigravityOut := filepath.Join(outBase, "antigravity")
	if err := (antigravityAdapter{}).Emit(m, repoRoot, antigravityOut); err != nil {
		t.Fatalf("emit antigravity: %v", err)
	}
	for _, dir := range []string{
		filepath.Join(codexOut, m.Targets["codex"].PluginSubdir, "skills"),
		filepath.Join(codexOut, m.Targets["codex"].PluginSubdir, "commands"),
		filepath.Join(antigravityOut, "skills"),
		filepath.Join(antigravityOut, "commands"),
	} {
		if files := skillFilesUnder(dir); len(files) > 0 {
			t.Errorf("%s should hold no file, found %v", dir, files)
		}
	}
}

// TestAntigravityEmitsMCPConfig verifies the antigravity adapter scaffolds a
// plugin-scoped mcp_config.json with an mcpServers map. agy reads plugin MCP
// servers exclusively from mcp_config.json at the extension root (verified live
// against agy v1.0.8 — spk-0698d585).
func TestAntigravityEmitsMCPConfig(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))

	target, ok := m.Targets["antigravity"]
	if !ok {
		t.Fatalf("manifest missing antigravity target")
	}
	if target.MCPPath != "mcp_config.json" {
		t.Fatalf("antigravity mcpPath = %q, want mcp_config.json", target.MCPPath)
	}

	out := t.TempDir()
	if err := (antigravityAdapter{}).Emit(m, repoRoot, out); err != nil {
		t.Fatalf("emit antigravity: %v", err)
	}

	mcpPath := filepath.Join(out, target.MCPPath)
	raw, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("expected mcp_config.json at %s: %v", mcpPath, err)
	}
	var parsed struct {
		MCPServers map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("mcp_config.json is not valid JSON: %v", err)
	}
	if parsed.MCPServers == nil {
		t.Errorf("mcp_config.json missing mcpServers key; got: %s", raw)
	}
}

// agyHookSpec mirrors the Antigravity (agy) JSONHookSpec shape that agy's
// parser accepts (verified live vs agy v1.0.8, feat-c08b20a6). It is used with
// DisallowUnknownFields so the test fails if the generator emits any key agy
// would reject — in particular the old Claude/Gemini event names
// (BeforeTool/AfterTool/BeforeAgent/AfterAgent/SessionStart/SessionEnd), which
// would surface as unknown fields and fail strict decode exactly as agy does.
type agyHookSpec struct {
	Enabled        bool                 `json:"enabled"`
	PreToolUse     []claudeMatcherGroup `json:"PreToolUse,omitempty"`
	PostToolUse    []claudeMatcherGroup `json:"PostToolUse,omitempty"`
	PreInvocation  []claudeMatcherGroup `json:"PreInvocation,omitempty"`
	PostInvocation []claudeMatcherGroup `json:"PostInvocation,omitempty"`
	Stop           []claudeMatcherGroup `json:"Stop,omitempty"`
}

// TestAntigravityHooksSchema asserts the generated Antigravity hooks.json uses
// the agy schema (named hook at top level with an "enabled" flag and per-event
// matcher-group arrays) and agy event names — not the Claude/Gemini schema that
// agy rejects with "cannot unmarshal array into JSONHookSpec".
func TestAntigravityHooksSchema(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))
	target := m.Targets["antigravity"]

	out := t.TempDir()
	if err := (antigravityAdapter{}).Emit(m, repoRoot, out); err != nil {
		t.Fatalf("emit antigravity: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(out, target.HooksPath))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}

	// Strict decode into the agy shape: top-level map of named hooks -> spec.
	// DisallowUnknownFields makes any stray key (e.g. a leaked Gemini event
	// name) fail exactly like agy's strict parser.
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var top map[string]agyHookSpec
	if err := dec.Decode(&top); err != nil {
		t.Fatalf("generated hooks.json does not match agy schema (strict decode failed): %v\n%s", err, raw)
	}

	spec, ok := top["wipnote"]
	if !ok {
		t.Fatalf("expected a top-level \"wipnote\" named hook; got keys %v", keysOf(top))
	}
	if !spec.Enabled {
		t.Error("named hook \"wipnote\" must have enabled:true")
	}
	// The four agy events that register handlers must all be present and wired
	// to a wipnote hook command. PostInvocation carried only after-agent, a
	// no-op removed in feat-174dc958.
	for name, groups := range map[string][]claudeMatcherGroup{
		"PreToolUse":    spec.PreToolUse,
		"PostToolUse":   spec.PostToolUse,
		"PreInvocation": spec.PreInvocation,
		"Stop":          spec.Stop,
	} {
		if len(groups) == 0 {
			t.Errorf("agy event %q missing from generated hooks.json", name)
			continue
		}
		if len(groups[0].Hooks) == 0 || !strings.Contains(groups[0].Hooks[0].Command, "wipnote hook ") {
			t.Errorf("agy event %q is not wired to a wipnote hook command: %+v", name, groups[0])
		}
	}

	// No Gemini-CLI event names may appear anywhere in the output.
	for _, bad := range []string{"BeforeTool", "AfterTool", "BeforeAgent", "AfterAgent", "AfterModel", "\"SessionStart\"", "\"SessionEnd\""} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("generated antigravity hooks.json must not contain Gemini/Claude event name %q:\n%s", bad, raw)
		}
	}
}

// TestAntigravityAgentToolRename asserts the emitted agent frontmatter uses
// agy's run_command, never the Gemini-CLI run_shell_command.
func TestAntigravityAgentToolRename(t *testing.T) {
	manifestPath, err := FindManifest(".")
	if err != nil {
		t.Skipf("no live manifest: %v", err)
	}
	m, err := Load(manifestPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(manifestPath)))

	out := t.TempDir()
	if err := (antigravityAdapter{}).Emit(m, repoRoot, out); err != nil {
		t.Fatalf("emit antigravity: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(out, "agents"))
	if err != nil {
		t.Fatalf("read agents dir: %v", err)
	}
	sawRunCommand := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		body, err := os.ReadFile(filepath.Join(out, "agents", e.Name()))
		if err != nil {
			t.Fatalf("read agent %s: %v", e.Name(), err)
		}
		if strings.Contains(string(body), "run_shell_command") {
			t.Errorf("agent %s contains Gemini tool run_shell_command; agy expects run_command", e.Name())
		}
		if strings.Contains(string(body), "run_command") {
			sawRunCommand = true
		}
	}
	if !sawRunCommand {
		t.Error("expected at least one antigravity agent to reference run_command (Bash translation)")
	}
}

func keysOf[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
