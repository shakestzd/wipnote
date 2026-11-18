package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesRemovedSkills checks that live guidance and code no
// longer point at the agent-context and code-quality skills, which the worker
// rules block replaced (feat-0b0112a4). Excluded: CLAUDE.md and CHANGELOG.md
// (owner and release notes), .wipnote/ (the store, never hand-edited),
// historical design notes and reviews under docs/, the generated
// docs/code-areas.md listing, site/ (a built search index), and this file.
func TestNoTrackedFileNamesRemovedSkills(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `agent-context|code-quality-skill`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range removedSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name a removed skill:\n%s", firstLines(string(out), 20))
	}
}

// removedSkillAllowed lists the paths that may still name the removed skills.
var removedSkillAllowed = []string{
	"CLAUDE.md", "CHANGELOG.md", ".wipnote", "docs/archive", "docs/code-areas.md",
	"docs/design", "docs/codex-interoperability-review.md", "site",
	"cmd/wipnote/preloaded_skills_removed_test.go",
}
