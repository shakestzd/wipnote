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
func inlinePreloadedSkills(raw []byte, skillsDir string) ([]byte, error) {
	fm, body, hasFM, err := parseAgentFrontmatter(raw)
	if err != nil || !hasFM {
		return raw, err
	}
	names := toStringSlice(fm["skills"])
	if len(names) == 0 {
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
	out.WriteString("\n")
	return out.Bytes(), nil
}
