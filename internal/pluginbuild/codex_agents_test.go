package pluginbuild

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestTranslateCodexAgentTOML(t *testing.T) {
	raw := []byte(`---
name: feature-coder
description: Balanced code execution agent
model: sonnet
color: blue
tools:
  - Read
  - Edit
  - Write
maxTurns: 60
skills:
  - agent-context
initialPrompt: "Run wipnote agent-init."
---

# Feature Coder Agent

Implement code changes.
`)

	agent, err := translateCodexAgent("feature-coder.md", raw)
	if err != nil {
		t.Fatalf("translateCodexAgent: %v", err)
	}

	if agent.Name != "wipnote-feature-coder" {
		t.Errorf("name = %q, want wipnote-feature-coder", agent.Name)
	}
	if agent.Description != "Balanced code execution agent" {
		t.Errorf("description = %q", agent.Description)
	}
	if agent.Model != "gpt-5.4" || agent.ModelReasoningEffort != "medium" {
		t.Errorf("Claude model alias should map to Codex model, got model=%q effort=%q", agent.Model, agent.ModelReasoningEffort)
	}
	if agent.SandboxMode != "workspace-write" {
		t.Errorf("sandbox_mode = %q, want workspace-write", agent.SandboxMode)
	}
	if len(agent.NicknameCandidates) != 1 || agent.NicknameCandidates[0] != "feature-coder" {
		t.Errorf("nickname_candidates = %#v", agent.NicknameCandidates)
	}
	if !strings.Contains(agent.DeveloperInstructions, "# Feature Coder Agent") {
		t.Errorf("developer_instructions missing body:\n%s", agent.DeveloperInstructions)
	}
	if !strings.Contains(agent.DeveloperInstructions, "## Initial Prompt\n\nRun wipnote agent-init.") {
		t.Errorf("developer_instructions missing initial prompt:\n%s", agent.DeveloperInstructions)
	}
}

func TestTranslateCodexAgentReaderSandbox(t *testing.T) {
	raw := []byte(`---
name: reader
description: Read-only file retrieval agent
model: haiku
tools:
  - Read
  - Grep
  - Glob
maxTurns: 10
---

# Reader Agent
`)

	agent, err := translateCodexAgent("reader.md", raw)
	if err != nil {
		t.Fatalf("translateCodexAgent: %v", err)
	}
	if agent.SandboxMode != "read-only" {
		t.Errorf("sandbox_mode = %q, want read-only", agent.SandboxMode)
	}
}

func TestMapCodexAgentModelAliases(t *testing.T) {
	tests := []struct {
		name       string
		model      string
		wantModel  string
		wantEffort string
	}{
		{name: "fast", model: "haiku", wantModel: "gpt-5.4-mini", wantEffort: "low"},
		{name: "balanced", model: "sonnet", wantModel: "gpt-5.4", wantEffort: "medium"},
		{name: "deep", model: "opus", wantModel: "gpt-5.5", wantEffort: "high"},
		{name: "native", model: "gpt-5.3-codex", wantModel: "gpt-5.3-codex", wantEffort: ""},
		{name: "inherit", model: "", wantModel: "", wantEffort: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotModel, gotEffort := mapCodexAgentModel(tt.model)
			if gotModel != tt.wantModel || gotEffort != tt.wantEffort {
				t.Fatalf("mapCodexAgentModel(%q) = (%q, %q), want (%q, %q)", tt.model, gotModel, gotEffort, tt.wantModel, tt.wantEffort)
			}
		})
	}
}

func TestCodexAdapterEmitsAgentsAsTOML(t *testing.T) {
	repoRoot := t.TempDir()
	seedAssets(t, repoRoot)
	agentDir := filepath.Join(repoRoot, "plugin", "agents")
	if err := os.WriteFile(filepath.Join(agentDir, "feature-coder.md"), []byte(`---
name: feature-coder
description: Balanced code execution agent
tools:
  - Read
  - Edit
---

# Feature Coder Agent
`), 0o644); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(repoRoot, "packages", "codex-marketplace")
	if err := (codexAdapter{}).Emit(fixtureManifest(), repoRoot, outDir); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	pluginDir := filepath.Join(outDir, ".agents", "plugins", "wipnote")
	if _, err := os.Stat(filepath.Join(pluginDir, "agents", "feature-coder.md")); !os.IsNotExist(err) {
		t.Errorf("Codex agents must not be copied as markdown; stat err=%v", err)
	}

	data, err := os.ReadFile(filepath.Join(pluginDir, "agents", "wipnote-feature-coder.toml"))
	if err != nil {
		t.Fatalf("read emitted agent TOML: %v", err)
	}
	agent, err := parseCodexAgentTOML(data)
	if err != nil {
		t.Fatalf("parse emitted agent TOML: %v", err)
	}
	if agent.Name != "wipnote-feature-coder" || agent.DeveloperInstructions == "" {
		t.Errorf("emitted agent mismatch: %+v", agent)
	}
}

func parseCodexAgentTOML(raw []byte) (codexAgentTOML, error) {
	dec := toml.NewDecoder(bytes.NewReader(raw))
	var agent codexAgentTOML
	if err := dec.Decode(&agent); err != nil {
		return codexAgentTOML{}, err
	}
	return agent, nil
}
