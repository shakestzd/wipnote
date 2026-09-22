package main

import (
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
)

// Already-fixed advisory on start (feat-89d7b057 / GH-#178).
//
// Fixing and closing are separate acts, and the second gets skipped when a
// fix lands as part of larger work. `start` then claims an item that is
// already done, and a whole dispatch is spent rediscovering that. The
// evidence is usually already on disk: commits linked to the item (by edge or
// by message), or the item id cited in a source comment next to the fix.
// Both checks are cheap, non-blocking, and printed to stderr so the operator
// looks before dispatching.

const (
	startAdvisoryMaxCommits = 5
	startAdvisoryMaxHits    = 10
)

// emitAlreadyFixedAdvisory prints the two signals for id to w when present.
// Silent when neither fires, when repoRoot is not a git repo, or when git is
// unavailable. Never returns an error: it must not affect the start.
func emitAlreadyFixedAdvisory(w io.Writer, repoRoot, id string, node *models.Node) {
	if repoRoot == "" || id == "" || !isGitRepo(repoRoot) {
		return
	}
	if commits := canonicalLinkedCommits(repoRoot, id, node); len(commits) > 0 {
		fmt.Fprintf(w, "start advisory: %s already has %d linked commit(s) — it may be fixed but never closed:\n", id, len(commits))
		for i, sha := range commits {
			if i == startAdvisoryMaxCommits {
				fmt.Fprintf(w, "  … and %d more\n", len(commits)-i)
				break
			}
			fmt.Fprintf(w, "  %s  %s\n", truncate(sha, 12), commitSubject(repoRoot, sha))
		}
	}
	if hits := sourceMentionsOfID(repoRoot, id); len(hits) > 0 {
		fmt.Fprintf(w, "start advisory: %s is cited in %d source file(s) outside .wipnote/ — check whether the fix already landed:\n", id, len(hits))
		for i, p := range hits {
			if i == startAdvisoryMaxHits {
				fmt.Fprintf(w, "  … and %d more\n", len(hits)-i)
				break
			}
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
}

// commitSubject returns sha's subject line, or "" when the object is not in
// this checkout (a committed_in edge to an unfetched commit).
func commitSubject(repoRoot, sha string) string {
	out, err := gitOutputIn(repoRoot, "log", "-1", "--format=%s", sha)
	if err != nil {
		return ""
	}
	return out
}

// sourceMentionsOfID lists tracked and untracked files under repoRoot,
// outside .wipnote/, that contain id verbatim. Non-fatal: any git failure
// (including "no match", exit 1) reads as no hits.
func sourceMentionsOfID(repoRoot, id string) []string {
	out, err := exec.Command(
		"git", "-C", repoRoot, "grep", "-l", "--untracked", "--fixed-strings",
		"-e", id, "--", ".", ":!.wipnote",
	).Output()
	if err != nil {
		return nil
	}
	var hits []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		p := filepath.ToSlash(strings.TrimSpace(line))
		if p == "" || isWipnotePath(p) {
			continue
		}
		hits = append(hits, p)
	}
	return hits
}
