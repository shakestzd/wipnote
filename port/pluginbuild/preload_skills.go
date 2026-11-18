package pluginbuild

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// inlinePreloadedSkills appends the body of every skill an agent lists in its
// `skills:` frontmatter to the agent's markdown body. Claude Code preloads
// those skills natively, but Codex and Antigravity drop the field (see
// agentFrontmatterFieldSpecs), so without this their agents would silently
// lose the shared rules (safety, attribution, quality gates) that the Claude
// agents receive. A listed skill that does not exist fails the build.
//
// Every "-coder" agent also gets the worker rules block at workerRulesPath,
// once (feat-0d9b57bc: one 3 KB block replaces 10 KB of skill text).
func inlinePreloadedSkills(raw []byte, skillsDir, workerRulesPath string) ([]byte, error) {
	fm, body, hasFM, err := parseAgentFrontmatter(raw)
	if err != nil || !hasFM {
		return raw, err
	}
	names := toStringSlice(fm["skills"])
	// Coder agents no longer list the two skills. Claude Code delivers the
	// block through the SubagentStart hook, but Codex and Antigravity have no
	// such hook, so the build inlines it for them (feat-0b0112a4).
	agentName, _ := fm["name"].(string)
	needRules := strings.HasSuffix(agentName, "-coder")
	if len(names) == 0 && !needRules {
		return raw, nil
	}
	fmRaw, _, _ := splitFrontmatter(raw)
	var out bytes.Buffer
	out.WriteString("---\n" + fmRaw + "\n---\n")
	out.Write(bytes.TrimRight(body, "\n"))
	for _, name := range names {
		dir := strings.TrimPrefix(name, "wipnote:")
		skill, err := os.ReadFile(filepath.Join(skillsDir, dir, "SKILL.md"))
		if err != nil {
			return nil, fmt.Errorf("preloaded skill %q: %w", name, err)
		}
		_, skillBody, _ := splitFrontmatter(skill)
		fmt.Fprintf(&out, "\n\n## Preloaded skill: %s\n\n%s", dir, strings.TrimSpace(string(skillBody)))
	}
	if needRules {
		rules, err := os.ReadFile(workerRulesPath)
		if err != nil {
			return nil, fmt.Errorf("worker rules: %w", err)
		}
		fmt.Fprintf(&out, "\n\n%s", strings.TrimSpace(string(rules)))
	}
	out.WriteString("\n")
	return out.Bytes(), nil
}

// workerRulesFile is the block's path under the repo root.
func workerRulesFile(repoRoot string) string {
	return filepath.Join(repoRoot, "cmd", "wipnote", "prompts", "worker-rules.md")
}
