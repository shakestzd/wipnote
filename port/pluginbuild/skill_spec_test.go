package pluginbuild

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Rules from Anthropic's skill authoring best practices
// (https://platform.claude.com/docs/en/agents-and-tools/agent-skills/best-practices).
// Every shared skill ships to Claude Code, Codex and Antigravity, so it must
// meet the Agent Skills spec, not only what Claude Code happens to tolerate.
var (
	skillNamePattern   = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	skillFirstPerson   = regexp.MustCompile(`(?i)\b(I can|I will|I'll|you can|you should)\b`)
	skillMarkdownLink  = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	skillMaxBodyLines  = 500  // "Keep SKILL.md body under 500 lines"
	skillMaxDescChars  = 1024 // frontmatter limit
	skillTOCAfterLines = 100  // "reference files longer than 100 lines" need a table of contents
)

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

func TestSkillsFollowAuthoringSpec(t *testing.T) {
	skillsDir, dirs := sharedSkillDirs(t)
	for _, dir := range dirs {
		t.Run(dir, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(skillsDir, dir, "SKILL.md"))
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			fmText, body, ok := splitFrontmatter(raw)
			if !ok {
				t.Fatal("SKILL.md has no YAML frontmatter")
			}
			var fm skillFrontmatter
			if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
				t.Fatalf("frontmatter does not parse: %v", err)
			}

			// name: lowercase/digits/hyphens, no reserved words, equal to the
			// directory so every harness resolves the same command.
			if fm.Name != dir {
				t.Errorf("name %q must equal the directory name %q", fm.Name, dir)
			}
			if !skillNamePattern.MatchString(fm.Name) {
				t.Errorf("name %q must be 1-64 lowercase letters, digits or hyphens", fm.Name)
			}
			if strings.Contains(fm.Name, "claude") || strings.Contains(fm.Name, "anthropic") {
				t.Errorf("name %q contains a reserved word", fm.Name)
			}

			// description: non-empty, <=1024 chars, no XML, third person, says when.
			desc := strings.TrimSpace(fm.Description)
			switch {
			case desc == "":
				t.Error("description is empty")
			case len(desc) > skillMaxDescChars:
				t.Errorf("description is %d chars; max %d", len(desc), skillMaxDescChars)
			}
			if strings.ContainsAny(desc, "<>") {
				t.Error("description must not contain XML tags")
			}
			if m := skillFirstPerson.FindString(desc); m != "" {
				t.Errorf("description must be third person; found %q", m)
			}
			if !strings.Contains(desc, "Use ") {
				t.Error(`description must say when to use the skill ("Use when ...")`)
			}

			text := string(body)
			if n := strings.Count(text, "\n"); n > skillMaxBodyLines {
				t.Errorf("body is %d lines; split it below %d", n, skillMaxBodyLines)
			}
			// The body loads only after the skill is chosen, so trigger lists
			// there never help discovery. Put triggers in the description.
			if strings.Contains(text, "Trigger keywords:") || strings.Contains(text, "Trigger situations:") {
				t.Error("body has a trigger list; move triggers into the description")
			}
			// Repo-relative paths do not exist once the plugin is installed.
			if strings.Contains(text, "plugin/skills/") {
				t.Error("body uses a repo-relative plugin/skills/ path; link files relative to the skill directory")
			}
			assertSkillLinksResolve(t, filepath.Join(skillsDir, dir), text)
		})
	}
}

// assertSkillLinksResolve checks that every relative markdown link in a skill
// points at a file in the skill directory, one level deep, and that long
// reference files carry a table of contents.
func assertSkillLinksResolve(t *testing.T, skillDir, text string) {
	t.Helper()
	for _, m := range skillMarkdownLink.FindAllStringSubmatch(text, -1) {
		target := strings.TrimPrefix(m[1], "./")
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") {
			continue
		}
		if i := strings.Index(target, "#"); i >= 0 {
			target = target[:i]
		}
		if strings.Contains(target, "\\") {
			t.Errorf("link %q uses a Windows-style path", m[1])
			continue
		}
		path := filepath.Join(skillDir, target)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("link %q does not resolve inside the skill directory", m[1])
			continue
		}
		if strings.Count(string(raw), "\n") > skillTOCAfterLines && !strings.Contains(string(raw), "## Contents") {
			t.Errorf("reference file %s is over %d lines and has no \"## Contents\" section", target, skillTOCAfterLines)
		}
	}
}

func TestCopyAssetTreesSkipArchived(t *testing.T) {
	src := t.TempDir()
	for _, rel := range []string{"live/SKILL.md", ".archived/old/SKILL.md"} {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("---\nname: x\n---\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	copiers := map[string]func(src, dst string) error{
		"claude":      copyAssetTree,
		"codex":       func(s, d string) error { return copyAssetTreeCodex(s, d, nil) },
		"antigravity": func(s, d string) error { return copyAssetTreeGemini(s, d, nil) },
	}
	for name, copyTree := range copiers {
		dst := t.TempDir()
		if err := copyTree(src, dst); err != nil {
			t.Fatalf("%s: copy: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dst, "live", "SKILL.md")); err != nil {
			t.Errorf("%s: live skill was not copied: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(dst, ".archived")); !os.IsNotExist(err) {
			t.Errorf("%s: .archived was copied into the target tree", name)
		}
	}
}
