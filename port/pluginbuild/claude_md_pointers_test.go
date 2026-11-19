package pluginbuild

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The proposed CLAUDE.md and its patch are slice 12 of plan-2622b3f7
// (feat-6f99bc47). Agents may not write CLAUDE.md, so the owner applies the
// patch. These tests keep the proposal honest until then.

// newRuleFiles are the path-scoped rules that slice 12 adds.
var newRuleFiles = []string{
	".claude/rules/plugin-source-layering.md",
	".claude/rules/worktree-artifacts.md",
}

// movedDocs hold the sections that fit no path.
var movedDocs = []string{
	"docs/guides/launching.md",
	"docs/guides/codespaces.md",
	"docs/runbook/upstream-monitoring.md",
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "packages", "plugin-core", "manifest.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return string(b)
}

var pathToken = regexp.MustCompile("`([A-Za-z0-9_./-]+(?:/[A-Za-z0-9_.-]+|\\.(?:md|sh|json|yml|yaml|go)))`")

func TestProposedClaudeMdPointersResolve(t *testing.T) {
	root := repoRootForTest(t)
	text := readRepoFile(t, root, "docs/design/claude-md-proposed.md")
	found := 0
	for _, m := range pathToken.FindAllStringSubmatch(text, -1) {
		p := m[1]
		if strings.HasPrefix(p, "~") || strings.Contains(p, "X.Y.Z") {
			continue
		}
		found++
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("proposed CLAUDE.md names %q, which is not in the tree", p)
		}
	}
	if found < 5 {
		t.Errorf("only %d path tokens found, the check is not reading the file", found)
	}
}

func TestProposedClaudeMdShape(t *testing.T) {
	root := repoRootForTest(t)
	text := readRepoFile(t, root, "docs/design/claude-md-proposed.md")
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if lines[0] != "@AGENTS.md" {
		t.Errorf("first line = %q, want @AGENTS.md", lines[0])
	}
	if n := len(lines); n < 30 || n > 50 {
		t.Errorf("proposed file has %d lines, want about 40 (30 to 50)", n)
	}
	for _, want := range []string{"wipnote build", "scripts/go-test-streaming.sh", "scripts/deploy-all.sh", "wipnote check --gate", "wipnote snapshot --summary"} {
		if !strings.Contains(text, want) {
			t.Errorf("proposed file lacks %q", want)
		}
	}
	for _, gone := range []string{"code-quality-skill", "plugin/commands", "plugin/skills"} {
		if strings.Contains(text, gone) {
			t.Errorf("proposed file names deleted %q", gone)
		}
	}
}

func TestNewRulesHavePathsMatchingTrackedFiles(t *testing.T) {
	root := repoRootForTest(t)
	out, err := exec.Command("git", "-C", root, "ls-files").Output()
	if err != nil {
		t.Fatal(err)
	}
	tracked := strings.Split(string(out), "\n")
	for _, rule := range newRuleFiles {
		globs := rulePaths(t, readRepoFile(t, root, rule), rule)
		for _, g := range globs {
			if !anyTrackedMatches(tracked, g) {
				t.Errorf("%s: paths entry %q matches no tracked file", rule, g)
			}
		}
	}
}

func TestMovedDocsExist(t *testing.T) {
	root := repoRootForTest(t)
	for _, d := range movedDocs {
		if len(strings.TrimSpace(readRepoFile(t, root, d))) < 200 {
			t.Errorf("%s is nearly empty", d)
		}
	}
}

func TestClaudeMdPatchApplies(t *testing.T) {
	root := repoRootForTest(t)
	if readRepoFile(t, root, "CLAUDE.md") == readRepoFile(t, root, "docs/design/claude-md-proposed.md") {
		t.Skip("the owner already applied the patch")
	}
	cmd := exec.Command("git", "-C", root, "apply", "--check", "docs/design/claude-md.patch")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Errorf("git apply --check failed: %v\n%s", err, out)
	}
}

// rulePaths reads the paths list from a rule file's frontmatter.
func rulePaths(t *testing.T, text, name string) []string {
	t.Helper()
	parts := strings.SplitN(text, "---\n", 3)
	if len(parts) < 3 || !strings.Contains(parts[1], "paths:") {
		t.Fatalf("%s has no paths frontmatter", name)
	}
	var globs []string
	for _, l := range strings.Split(parts[1], "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "- ") {
			globs = append(globs, strings.Trim(strings.TrimPrefix(l, "- "), `"'`))
		}
	}
	if len(globs) == 0 {
		t.Fatalf("%s has an empty paths list", name)
	}
	return globs
}

// anyTrackedMatches treats the glob as a prefix up to its first star, plus a
// suffix after the last star when the glob has a file extension.
func anyTrackedMatches(tracked []string, glob string) bool {
	i := strings.Index(glob, "*")
	if i < 0 {
		for _, f := range tracked {
			if f == glob {
				return true
			}
		}
		return false
	}
	prefix := glob[:i]
	suffix := glob[strings.LastIndex(glob, "*")+1:]
	for _, f := range tracked {
		if strings.HasPrefix(f, prefix) && strings.HasSuffix(f, suffix) {
			return true
		}
	}
	return false
}
