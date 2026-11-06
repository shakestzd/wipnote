package worktree

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
)

// worktreesExcludeLine is what keeps .claude/worktrees out of git status.
const worktreesExcludeLine = "/.claude/worktrees/"

// ExcludeWorktreesDir keeps .claude/worktrees from dirtying a repo served by an
// external store, whose promise is that the repo stays clean. It writes to the
// repo's own info/exclude, unless the repo already ignores the folder. An
// in-repo store's repo manages its own ignore rules and is left alone.
func ExcludeWorktreesDir(repoRoot string) {
	if paths.ExternalStoreFor(repoRoot) == "" {
		return
	}
	if _, err := git(repoRoot, "check-ignore", "-q", ".claude/worktrees/x"); err == nil {
		return
	}
	commonDir, err := git(repoRoot, "rev-parse", "--git-common-dir")
	if err != nil {
		return
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(repoRoot, commonDir)
	}
	path := filepath.Join(commonDir, "info", "exclude")
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.TrimSpace(line) == worktreesExcludeLine {
			return
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("\n" + worktreesExcludeLine + "\n")
}
