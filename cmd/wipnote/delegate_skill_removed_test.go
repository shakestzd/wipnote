package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesDelegateSkill checks that live guidance and code no
// longer point at the deleted delegate skill (feat-8f910039). Excluded:
// CLAUDE.md and CHANGELOG.md (owner and release notes), .wipnote/ (the store,
// never hand-edited), historical design notes and reviews under docs/, the
// generated docs/code-areas.md listing, site/ (a built search index), and this
// file.
func TestNoTrackedFileNamesDelegateSkill(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `wipnote:delegate([^-a-z ]|$)|skill show delegate([^-]|$)|skills/delegate/|delegate skill`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range delegateSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name the delegate skill:\n%s", firstLines(string(out), 20))
	}
}

// delegateSkillAllowed lists the paths that may still name the delegate skill.
var delegateSkillAllowed = []string{
	"CLAUDE.md", "CHANGELOG.md", ".wipnote", "docs/archive", "docs/code-areas.md",
	"docs/design", "docs/internal-plan-system-analysis.md",
	"docs/codex-interoperability-review.md", "site",
	"cmd/wipnote/delegate_skill_removed_test.go",
}
