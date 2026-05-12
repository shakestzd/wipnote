package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// testTmpComponentRe matches any single path component that looks like a Go
// test scratch directory name. These directories live inside the project tree
// (to avoid /tmp noexec in devcontainers) but must never trigger real git
// mutations during test runs.
var testTmpComponentRe = regexp.MustCompile(`^\.(test-tmp|tmp-gotest)$`)

// isTestTmpPath returns true when absPath contains a component matching
// testTmpComponentRe or begins with the well-known prefixes "tmp/go-tmp/" or
// "tmp/gotmp.". This is a defense-in-depth guard: even if a test
// mis-configures isolation, real git commits are prevented.
func isTestTmpPath(absPath string) bool {
	// Walk every component of the path.
	for p := absPath; ; {
		dir, base := filepath.Split(filepath.Clean(p))
		if testTmpComponentRe.MatchString(base) {
			return true
		}
		// Also catch absolute components like "/tmp/go-tmp/" by checking
		// the full remaining prefix.
		if strings.Contains(absPath, "/tmp/go-tmp/") || strings.Contains(absPath, "/tmp/gotmp.") {
			return true
		}
		if dir == p || dir == "" || dir == "." {
			break
		}
		p = dir
	}
	return false
}

// commitWipnoteArtifact stages and commits the work-item HTML file for the
// given typeName and id to the main git repository that contains wipnoteDir.
//
// typeName is "feature", "bug", or "spike"; the HTML lives at
// .wipnote/<typeName>s/<id>.html relative to the project root.
//
// Design: `git -C <repoRoot>` anchors the command to the main repository even
// when the caller's shell CWD is inside a linked worktree. The per-worktree
// gitignore installed by excludeWipnoteFromWorktree (internal/worktree) only
// affects the worktree's own `git status` display; explicit paths passed to
// `git add --` bypass the exclude entirely, so the file is staged and committed
// in the main repo regardless of CWD.
//
// Non-fatal contract: if the project is not in a git repo, or if git commit
// fails for any reason (hook rejection, locked index, nothing to commit), the
// function logs to stderr and returns nil. The caller must not make completion
// of the work item depend on the git commit succeeding.
func commitWipnoteArtifact(wipnoteDir, typeName, id string) error {
	// Derive the repo root: wipnoteDir is .wipnote/ inside the project root.
	repoRoot := filepath.Dir(wipnoteDir)

	// Defense-in-depth: reject paths that look like Go test scratch
	// directories. These live inside the project tree (see .gitignore comment
	// "Go test scratch space") but must never trigger real git mutations.
	// Silent skip — this is never a caller error, just a mis-configured test.
	absWipnote, err := filepath.Abs(wipnoteDir)
	if err != nil {
		absWipnote = wipnoteDir
	}
	if isTestTmpPath(absWipnote) {
		if os.Getenv("WIPNOTE_DEBUG") == "1" {
			fmt.Fprintf(stderr, "autocommit skipped: path looks like a test temp dir: %s\n", absWipnote)
		}
		return nil
	}

	if !isGitRepo(repoRoot) {
		fmt.Fprintf(stderr, "autocommit skipped: %s is not inside a git repository\n", repoRoot)
		return nil
	}

	subDir := typeName + "s"
	relPath := filepath.Join(".wipnote", subDir, id+".html")
	absPath := filepath.Join(wipnoteDir, subDir, id+".html")

	// Stage the file. Use an explicit path to avoid sweeping unrelated changes.
	addOut, err := exec.Command("git", "-C", repoRoot, "add", "--", absPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("autocommit: git add %s: %s: %w", relPath, strings.TrimSpace(string(addOut)), err)
	}

	// Check whether anything was staged by the add above. Use git diff --cached
	// on the specific file so we don't accidentally commit unrelated staged changes.
	diffOut, err := exec.Command("git", "-C", repoRoot, "diff", "--cached", "--quiet", "--", absPath).CombinedOutput()
	if err == nil {
		// Exit code 0 means no diff — nothing new to commit.
		return nil
	}
	_ = diffOut // non-zero exit is the expected "there is a diff" result

	// Commit only the artifact file — never touch the broader index.
	msg := "wipnote: complete " + id
	commitOut, err := exec.Command(
		"git", "-C", repoRoot, "commit", "-m", msg, "--", absPath,
	).CombinedOutput()
	if err != nil {
		outStr := string(commitOut)
		if strings.Contains(outStr, "nothing to commit") || strings.Contains(outStr, "no changes added") {
			return nil
		}
		fmt.Fprintf(stderr, "autocommit warning: git commit failed for %s (artifact persisted to disk — please commit manually): %s\n",
			id, strings.TrimSpace(outStr))
		return nil
	}

	return nil
}
