package main

import (
	"os/exec"
	"strings"
	"testing"
)

// TestNoTrackedFileNamesCritiqueSkill checks that live guidance and code no
// longer point at the deleted plan-critique skill (feat-0be892a9). The same
// paths are excluded as in the planning-skill check, plus this file.
func TestNoTrackedFileNamesCritiqueSkill(t *testing.T) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Skipf("not a git checkout: %v", err)
	}
	dir := strings.TrimSpace(string(root))
	pattern := `wipnote:plan-critique|skill show plan-critique|skills/plan-critique`
	args := []string{"-C", dir, "grep", "-nIiE", pattern, "--", "."}
	for _, x := range critiqueSkillAllowed {
		args = append(args, ":(exclude)"+x)
	}
	out, _ := exec.Command("git", args...).Output()
	if len(out) > 0 {
		t.Errorf("tracked files name the plan-critique skill:\n%s", firstLines(string(out), 20))
	}
}

// critiqueSkillAllowed lists the paths that may still name the skill.
var critiqueSkillAllowed = append(append([]string{}, planSkillAllowed...),
	"cmd/wipnote/plan_critique_skill_removed_test.go")
