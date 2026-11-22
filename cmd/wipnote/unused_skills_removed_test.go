package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesUnusedSkills checks that live guidance and code no
// longer point at the last skills and slash commands, removed by feat-9796e97f
// because no transcript loaded them by hand. Excluded: CLAUDE.md and
// CHANGELOG.md (owner and release notes), .wipnote/ (the store, never
// hand-edited), historical notes under docs/, the generated docs/code-areas.md
// listing, site/ (a built search index), and this file.
func TestNoTrackedFileNamesUnusedSkills(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `guard-authoring|ui-stills|report-issue|simplified-technical-english|ste_lint|` +
		`skill show|skillCmd|skills/skills|wipnote:(ci|init|plan-review|recommend|skills)([^-a-z]|$)`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range unusedSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name a removed skill or command:\n%s", firstLines(string(out), 20))
	}
}

// TestUnusedSkillFoldersAreEmpty checks that no tracked file remains under the
// two folders that held the skills and slash commands.
func TestUnusedSkillFoldersAreEmpty(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	out, _ := exec.Command("git", "-C", dir, "ls-files", "plugin/skills", "plugin/commands").Output()
	if len(out) > 0 {
		t.Errorf("tracked files remain under plugin/skills or plugin/commands:\n%s", firstLines(string(out), 20))
	}
}

// unusedSkillAllowed lists the paths that may still name the removed items.
var unusedSkillAllowed = []string{
	"CLAUDE.md", "CHANGELOG.md", ".wipnote", "docs/archive", "docs/code-areas.md",
	"docs/design", "docs/codex-interoperability-review.md", "site",
	"cmd/wipnote/unused_skills_removed_test.go",
	// The earlier reference tests name `skill show <dir>` in their patterns.
	"cmd/wipnote/delegate_skill_removed_test.go", "cmd/wipnote/execute_skill_removed_test.go",
	"cmd/wipnote/plan_critique_skill_removed_test.go", "cmd/wipnote/plan_skill_removed_test.go",
}
