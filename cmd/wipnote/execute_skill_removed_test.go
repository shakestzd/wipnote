package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesExecuteSkill checks that live guidance and code no
// longer point at the deleted execute skill or at wipnote:cleanup, a command
// that never existed (feat-d06e3fb2). Excluded: CLAUDE.md and CHANGELOG.md
// (owner and release notes), .wipnote/ (the store, never hand-edited),
// historical notes under docs/, the generated docs/code-areas.md listing,
// site/ (a built search index), and this file.
func TestNoTrackedFileNamesExecuteSkill(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `wipnote:execute|wipnote:cleanup|skills/execute|skill show execute|execute skill|execute-(defers|fires|ignores)`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range executeSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name the execute skill:\n%s", firstLines(string(out), 30))
	}
}

// executeSkillAllowed lists the paths that may still name the execute skill.
var executeSkillAllowed = []string{
	"CLAUDE.md", "CHANGELOG.md", ".wipnote", "docs/archive", "docs/code-areas.md",
	"docs/design", "docs/codex-interoperability-review.md", "site",
	"cmd/wipnote/execute_skill_removed_test.go",
}
