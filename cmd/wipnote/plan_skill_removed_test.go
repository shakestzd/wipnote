package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesPlanSkill checks that live guidance and code no longer
// point at the deleted plan skill (feat-bcc2297f). Excluded: CLAUDE.md and
// CHANGELOG.md (owner and release notes), .wipnote/ (the store, never
// hand-edited), historical design notes and reviews under docs/, the generated
// docs/code-areas.md listing, site/ (a built search index), and this file.
func TestNoTrackedFileNamesPlanSkill(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `wipnote:plan([^-a-z]|$)|skill show plan([^-]|$)|skills/plan/|plan skill|phase [0-9] of /wipnote`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range planSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name the plan skill:\n%s", firstLines(string(out), 20))
	}
}

// planSkillAllowed lists the paths that may still name the plan skill.
var planSkillAllowed = []string{
	"CLAUDE.md", "CHANGELOG.md", ".wipnote", "docs/archive", "docs/code-areas.md",
	"docs/design", "docs/internal-plan-system-analysis.md",
	"docs/codex-interoperability-review.md", "site",
	"cmd/wipnote/plan_skill_removed_test.go",
}

func firstLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
