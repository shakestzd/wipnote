package paths

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// errNoGitDir is returned when a git helper is called without a directory.
// The helpers never fall back to the process cwd: pass "." to mean it.
var errNoGitDir = errors.New("git: no directory given")

// GitTopLevel returns the top-level directory of the worktree that owns dir
// (`git -C dir rev-parse --show-toplevel`), independent of the process cwd.
func GitTopLevel(dir string) (string, error) {
	if dir == "" {
		return "", errNoGitDir
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git -C %s rev-parse --show-toplevel: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GitHead returns the full sha of HEAD in dir, or "" when it cannot be
// resolved (no repository, no commits, or no dir given).
func GitHead(dir string) string {
	if dir == "" {
		return ""
	}
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
