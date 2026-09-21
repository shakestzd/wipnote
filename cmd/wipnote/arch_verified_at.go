package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	corearch "github.com/shakestzd/wipnote/core/arch"
)

// commitStatus is the result of resolving a card's verified_at SHA against
// the repository (GH-#173).
type commitStatus int

const (
	// commitUnchecked: repoRoot is not a git repository (or git is missing),
	// so the SHA cannot be checked. Validation stays silent.
	commitUnchecked commitStatus = iota
	// commitOK: the commit exists and is an ancestor of HEAD.
	commitOK
	// commitMissing: no such commit object (e.g. purged by git filter-repo).
	commitMissing
	// commitUnreachable: the object exists but is not an ancestor of HEAD
	// (orphaned branch, rewritten history still in the reflog).
	commitUnreachable
)

// gitCommitReachable classifies sha for the repository at repoRoot using
// `git cat-file -e` (existence) and `git merge-base --is-ancestor` (reachable
// from HEAD).
func gitCommitReachable(repoRoot, sha string) commitStatus {
	if !isGitRepo(repoRoot) {
		return commitUnchecked
	}
	if err := exec.Command("git", "-C", repoRoot, "cat-file", "-e", sha+"^{commit}").Run(); err != nil {
		return commitMissing
	}
	if err := exec.Command("git", "-C", repoRoot, "merge-base", "--is-ancestor", sha, "HEAD").Run(); err != nil {
		return commitUnreachable
	}
	return commitOK
}

// verifiedAtProblem checks a card's verified_at and returns (errMsg, warnMsg):
// a missing commit is an error (the verification claim cannot be true), an
// unreachable one is a warning (the claim may hold on another branch). Both
// are "" when the card has no verified_at or the repo cannot be checked.
func verifiedAtProblem(repoRoot string, card *corearch.Card) (string, string) {
	if card == nil || strings.TrimSpace(card.VerifiedAt) == "" {
		return "", ""
	}
	switch gitCommitReachable(repoRoot, card.VerifiedAt) {
	case commitMissing:
		return fmt.Sprintf("verified_at %s does not exist in this repository (history rewritten? re-pin with 'arch edit %s --verified-at <sha>' or 'arch repair --commit-map <file>')", firstN(card.VerifiedAt, 12), card.Name), ""
	case commitUnreachable:
		return "", fmt.Sprintf("verified_at %s exists but is not reachable from HEAD", firstN(card.VerifiedAt, 12))
	}
	return "", ""
}

// reportVerifiedAt prints ERROR/WARN lines for card's verified_at and returns
// true when an ERROR was emitted.
func reportVerifiedAt(repoRoot string, card *corearch.Card) bool {
	errMsg, warnMsg := verifiedAtProblem(repoRoot, card)
	if warnMsg != "" {
		fmt.Fprintf(os.Stderr, "WARN card %s: %s\n", card.Name, warnMsg)
	}
	if errMsg != "" {
		fmt.Fprintf(os.Stderr, "ERROR card %s: %s\n", card.Name, errMsg)
		return true
	}
	return false
}

// checkEditVerifiedAt guards `arch edit --verified-at`: a SHA that does not
// exist is rejected unless force is set; an unreachable SHA only warns.
func checkEditVerifiedAt(repoRoot, sha string, force bool) error {
	if strings.TrimSpace(sha) == "" || force {
		return nil
	}
	switch gitCommitReachable(repoRoot, sha) {
	case commitMissing:
		return fmt.Errorf("--verified-at %s: commit does not exist in this repository (pass --force to set it anyway)", sha)
	case commitUnreachable:
		fmt.Fprintf(os.Stderr, "WARN --verified-at %s exists but is not reachable from HEAD\n", firstN(sha, 12))
	}
	return nil
}

// loadCommitMap parses a git-filter-repo style commit map: one "old new"
// pair per line, blank lines and '#' comments ignored. A leading header line
// ("old new", as filter-repo writes it) is skipped.
func loadCommitMap(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open commit map: %w", err)
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 {
			return nil, fmt.Errorf("commit map %s:%d: expected \"old new\", got %q", path, line, text)
		}
		if fields[0] == "old" && fields[1] == "new" {
			continue
		}
		out[fields[0]] = fields[1]
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read commit map: %w", err)
	}
	return out, nil
}

// applyCommitMap re-pins every active card whose verified_at appears in the
// map. It returns the number of cards rewritten and the number of failed
// writes. dryRun only prints what would change.
func applyCommitMap(store *corearch.Store, commitMap map[string]string, dryRun bool) (int, int, error) {
	cards, err := store.List(false)
	if err != nil {
		return 0, 0, err
	}
	repinned, failures := 0, 0
	for _, card := range cards {
		newSHA, ok := commitMap[card.VerifiedAt]
		if !ok || card.VerifiedAt == "" || newSHA == card.VerifiedAt {
			continue
		}
		fmt.Printf("card %s: verified_at %s -> %s\n", card.Name, firstN(card.VerifiedAt, 12), firstN(newSHA, 12))
		if dryRun {
			fmt.Printf("  (dry-run: no file written)\n")
			continue
		}
		card.VerifiedAt = newSHA
		if err := store.Update(card); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR repair %s: update failed: %v\n", card.Name, err)
			failures++
			continue
		}
		repinned++
	}
	return repinned, failures, nil
}
