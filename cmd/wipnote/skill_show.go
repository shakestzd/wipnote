package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/spf13/cobra"
)

var (
	skillDirRe  = regexp.MustCompile(`^[a-z0-9-]+$`)
	skillFileRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.md$`)
)

// skillCmd prints bundled skill files. Hidden skills (disable-model-invocation)
// are not in the model's skill list, and ${CLAUDE_PLUGIN_ROOT} only resolves on
// Claude Code, so prompts reach them through this harness-neutral command.
func skillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Read bundled wipnote skill files",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "show <skill-dir> [file]",
		Short: "Print a wipnote skill's SKILL.md (or another .md file in its folder)",
		Long: "Print a bundled wipnote skill file for the current harness.\n" +
			"Use it to load a skill that is hidden from the skill list, e.g. 'wipnote skill show plan'.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			file := "SKILL.md"
			if len(args) == 2 {
				file = args[1]
			}
			path, err := resolveSkillFile(agent.DetectEnvHarness(), args[0], file)
			if err != nil {
				return err
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	})
	return cmd
}

// resolveSkillFile finds <dir>/<file> in the current harness's bundled tree,
// falling back to the Claude plugin tree, which every install carries.
func resolveSkillFile(harness, dir, file string) (string, error) {
	if !skillDirRe.MatchString(dir) {
		return "", fmt.Errorf("invalid skill name %q: use the skill's folder name, e.g. plan", dir)
	}
	if !skillFileRe.MatchString(file) {
		return "", fmt.Errorf("invalid skill file %q: use a .md file name in the skill folder", file)
	}
	for _, tree := range skillTreesFor(harness) {
		root, err := resolveSharedTreePath(tree.name)
		if err != nil {
			continue
		}
		path := filepath.Join(root, tree.skillsSubdir, dir, file)
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("wipnote skill file %s/%s not found; run 'wipnote help --compact' or /wipnote:skills for the skill list", dir, file)
}

type skillTree struct{ name, skillsSubdir string }

func skillTreesFor(harness string) []skillTree {
	claude := skillTree{"plugin", "skills"}
	switch harness {
	case agent.HarnessCodex:
		return []skillTree{{"codex-marketplace", filepath.Join(".agents", "plugins", "wipnote", "skills")}, claude}
	case agent.HarnessAntigravity, agent.HarnessGemini:
		return []skillTree{{"antigravity-extension", "skills"}, claude}
	default:
		return []skillTree{claude}
	}
}
