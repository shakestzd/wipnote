package main

import (
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/paths"
)

// archiveGitPlan says whether `archive --apply` takes part in git for a store,
// and when it does not, why. It reuses the decisions the artifact commit path
// already makes: policy none skips commits, an app-owned (external) store has
// no code repo above it, and a path git ignores can never be staged
// (isGitIgnored, the probe behind ErrPathIgnored).
//
// Tracked files are never "ignored" to check-ignore, so a conductor/yolo
// worktree whose per-worktree exclude hides .wipnote/ still commits: its
// store files are tracked. Only a store git does not track AND ignores is
// treated as disk-only.
func archiveGitPlan(wipnoteDir string) (commit bool, reason string) {
	repoRoot := filepath.Dir(wipnoteDir)
	switch {
	case workitemArtifactCommitPolicyForEnv() == workitemArtifactCommitPolicyNone:
		return false, "WIPNOTE_ARTIFACT_COMMIT_POLICY=none"
	case paths.IsExternalStore(repoRoot):
		return false, "the store is app-owned and not in git"
	case !isGitRepo(repoRoot):
		return false, repoRoot + " is not inside a git repository"
	case gitTracksPath(repoRoot, ".wipnote"):
		return true, ""
	}
	probe := archiveLedgerRel(wipnoteDir)
	if rule, ignored := isGitIgnored(repoRoot, probe); ignored {
		return false, "git ignores " + probe + " (rule " + rule + ")"
	}
	return true, ""
}

// archiveLedgerRel is the repo-relative path of the first ledger, used to ask
// git whether the store is ignored.
func archiveLedgerRel(wipnoteDir string) string {
	ledger := graph.ArchiveLedgerPath(wipnoteDir, graph.ArchiveLedgerCollections[0])
	rel, err := filepath.Rel(filepath.Dir(wipnoteDir), ledger)
	if err != nil {
		return filepath.Join(".wipnote", "archive")
	}
	return rel
}

// gitTracksPath reports whether git tracks at least one file under relPath.
func gitTracksPath(repoRoot, relPath string) bool {
	out, err := exec.Command("git", "-C", repoRoot, "ls-files", "--", relPath).Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}
