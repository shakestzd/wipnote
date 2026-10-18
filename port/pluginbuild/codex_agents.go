package pluginbuild

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
)

// AgentAsset is the canonical markdown/frontmatter agent asset used by the
// shared plugin source tree. The source format follows Claude conventions; each
// target adapter maps only fields its host clearly supports.
type AgentAsset struct {
	Name          string   `yaml:"name"`
	Description   string   `yaml:"description"`
	Model         string   `yaml:"model"`
	Color         string   `yaml:"color"`
	Tools         []string `yaml:"tools"`
	Disallowed    []string `yaml:"disallowedTools"`
	MaxTurns      int      `yaml:"maxTurns"`
	Skills        []string `yaml:"skills"`
	InitialPrompt string   `yaml:"initialPrompt"`
}

type codexAgentTOML struct {
	Name                  string   `toml:"name"`
	Description           string   `toml:"description"`
	Model                 string   `toml:"model,omitempty"`
	ModelReasoningEffort  string   `toml:"model_reasoning_effort,omitempty"`
	NicknameCandidates    []string `toml:"nickname_candidates,omitempty"`
	DeveloperInstructions string   `toml:"developer_instructions"`
}

func emitCodexAgents(m *Manifest, repoRoot, outDir string, knownRoles map[string]struct{}) error {
	if m.AssetSources.Agents == "" {
		return nil
	}
	srcDir := filepath.Join(repoRoot, m.AssetSources.Agents)
	info, err := os.Stat(srcDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat agents source %s: %w", srcDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("agents source %s is not a directory", srcDir)
	}

	dstDir := filepath.Join(outDir, "agents")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read agents source %s: %w", srcDir, err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			return fmt.Errorf("read agent %s: %w", e.Name(), err)
		}
		if raw, err = inlinePreloadedSkills(raw, filepath.Join(repoRoot, m.AssetSources.Skills)); err != nil {
			return fmt.Errorf("agent %s: %w", e.Name(), err)
		}
		agent, err := translateCodexAgent(e.Name(), raw)
		if err != nil {
			return fmt.Errorf("translate agent %s: %w", e.Name(), err)
		}
		// Translate colon-form agent IDs in developer_instructions so agent
		// bodies that reference wipnote:<role> work correctly in Codex.
		agent.DeveloperInstructions = codexRewriteAgentIDs(agent.DeveloperInstructions, knownRoles)
		out, err := toml.Marshal(agent)
		if err != nil {
			return fmt.Errorf("marshal codex agent %s: %w", e.Name(), err)
		}
		dst := filepath.Join(dstDir, agent.Name+".toml")
		if err := os.WriteFile(dst, out, 0o644); err != nil {
			return fmt.Errorf("write codex agent %s: %w", dst, err)
		}
	}
	return nil
}

func translateCodexAgent(filename string, raw []byte) (codexAgentTOML, error) {
	fm, body, hasFM, err := parseAgentFrontmatter(raw)
	if err != nil {
		return codexAgentTOML{}, err
	}
	if !hasFM {
		name := strings.TrimSuffix(filename, filepath.Ext(filename))
		return codexAgentTOML{
			Name:                  codexAgentName(name),
			Description:           "wipnote agent",
			NicknameCandidates:    nicknameCandidates(name),
			DeveloperInstructions: withReadOnlyNotice(strings.TrimSpace(string(raw)), true),
		}, nil
	}

	filtered := filterAgentFrontmatter(filename, "codex", fm)
	filteredYAML, err := marshalAgentFrontmatter(filtered)
	if err != nil {
		return codexAgentTOML{}, err
	}
	var asset AgentAsset
	if err := yaml.Unmarshal(filteredYAML, &asset); err != nil {
		return codexAgentTOML{}, fmt.Errorf("parse frontmatter YAML: %w", err)
	}
	if asset.Name == "" {
		asset.Name = strings.TrimSuffix(filename, filepath.Ext(filename))
	}
	if asset.Description == "" {
		return codexAgentTOML{}, fmt.Errorf("description is required")
	}

	model, effort, err := mapCodexAgentModel(asset.Model)
	if err != nil {
		return codexAgentTOML{}, fmt.Errorf("map model %q: %w", asset.Model, err)
	}
	return codexAgentTOML{
		Name:                  codexAgentName(asset.Name),
		Description:           asset.Description,
		Model:                 model,
		ModelReasoningEffort:  effort,
		NicknameCandidates:    nicknameCandidates(asset.Name),
		DeveloperInstructions: withReadOnlyNotice(codexDeveloperInstructions(body, asset.InitialPrompt), codexReadOnlyRole(asset)),
	}, nil
}

func codexAgentName(name string) string {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "wipnote-") {
		return name
	}
	return "wipnote-" + name
}

func nicknameCandidates(name string) []string {
	if name == "" {
		return nil
	}
	return []string{name}
}

func codexDeveloperInstructions(body []byte, initialPrompt string) string {
	parts := []string{}
	if s := strings.TrimSpace(string(body)); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(initialPrompt); s != "" {
		parts = append(parts, "## Initial Prompt\n\n"+s)
	}
	return strings.Join(parts, "\n\n")
}

// codexReadOnlyRole reports whether an agent has no write-capable tool.
//
// Codex does not let a custom agent role set its sandbox: role files are read
// through an allowlist (developer_instructions, model, reasoning effort, ...;
// codex-rs/core/src/agent/role.rs apply_role_to_config) and sandbox_mode is
// dropped, so the `sandbox_mode = "read-only"` this generator used to emit was
// never enforced. The restriction is therefore stated in the instructions,
// which Codex does deliver.
func codexReadOnlyRole(asset AgentAsset) bool {
	for _, tool := range asset.Tools {
		switch tool {
		case "Edit", "Write", "Bash":
			return false
		}
	}
	return true
}

// codexReadOnlyNotice leads the developer instructions of a read-only role.
const codexReadOnlyNotice = "**Read-only role.** Codex does not sandbox agent roles, so this is yours to keep: " +
	"never create, edit, move or delete files, and never run a command that changes the repository or system state. " +
	"If a change is needed, report the exact change instead."

func withReadOnlyNotice(instructions string, readOnly bool) string {
	if !readOnly {
		return instructions
	}
	if instructions == "" {
		return codexReadOnlyNotice
	}
	return codexReadOnlyNotice + "\n\n" + instructions
}

func mapCodexAgentModel(model string) (string, string, error) {
	model = strings.TrimSpace(model)
	// Empty model means the field was absent; inherit harness default
	if model == "" {
		return "", "", nil
	}
	// Tier mapping against Codex's bundled model catalog
	// (codex-rs/models-manager/models.json). Re-verify with `codex debug models`
	// on each Codex release: gpt-5.5 was retired for ChatGPT sign-in on
	// 2026-10-14, and the catalog now marks gpt-5.6-luna/-terra as upgraded to
	// gpt-6-luna/gpt-6-sol.
	switch model {
	case "haiku":
		return "gpt-6-luna", "low", nil // "Fast and affordable model for easier tasks."
	case "sonnet":
		return "gpt-6.1-sol", "medium", nil // "Latest workhorse model for coding and everyday work."
	case "opus":
		return "gpt-6-astra", "high", nil // "Frontier intelligence for the most demanding work."
	}
	if strings.HasPrefix(model, "gpt-") {
		return model, "", nil
	}
	// Present but unrecognized alias is an error
	return "", "", fmt.Errorf("unknown model alias %q; use one of: haiku, sonnet, opus, or a gpt-* model ID", model)
}
