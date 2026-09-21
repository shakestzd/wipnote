package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// gitignore_advice.go — detection and explanation of the ".wipnote/ is
// gitignored" misconfiguration (GH#172, bug-e59a0d57).
//
// The privacy instinct behind ignoring .wipnote/ is correct: session
// transcripts under .wipnote/sessions/ contain user prompts verbatim. But the
// work-item artifacts (features/, bugs/, spikes/, tracks/) MUST reach git —
// every deferred artifact-commit intent names one, and an intent against an
// ignored path can never succeed. Without a probe the queue retried blind,
// dead-lettered real state, and `check --gate` pointed at `commit-queue flush`,
// the one action that cannot work. These helpers turn one `git check-ignore`
// call into a targeted explanation.

// wipnoteGitignoreAdvice is the remediation appended to every ignored-path
// message: keep prompts private by ignoring the session/telemetry paths by
// name instead of the whole tree. The nested-ignore caveat matters: a
// trailing-slash `.wipnote/` rule stops git descending, so a `!` negation
// inside is silently ignored; only `.wipnote/*` lets a negation apply.
const wipnoteGitignoreAdvice = "wipnote needs its work-item artifacts committed. " +
	"If you are ignoring .wipnote/ to keep prompts out of git, ignore " +
	".wipnote/sessions/ (and the other runtime paths listed in .wipnote/.gitignore) " +
	"instead of the whole tree. Note: a `.wipnote/` rule blocks any `!` negation " +
	"beneath it — use `.wipnote/*` with `!.wipnote/features/` etc. if you negate."

// isGitIgnored reports whether relPath (repo-relative) is ignored by git in
// repoRoot, and names the matching rule as "<source>:<line>:<pattern>". Only
// untracked paths can be ignored (git check-ignore skips tracked files), which
// is exactly the case where `git add` refuses the path. Any git error
// (not a repo, git missing) reads as "not ignored" so the probe can never
// block a commit that git itself would accept.
func isGitIgnored(repoRoot, relPath string) (rule string, ignored bool) {
	out, err := exec.Command("git", "-C", repoRoot, "check-ignore", "-v", "--", relPath).Output()
	if err != nil {
		return "", false // exit 1 = not ignored; 128 = not a repo/other error
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return "", false
	}
	// Format: <source>:<linenum>:<pattern><TAB><pathname>
	if tab := strings.IndexByte(line, '\t'); tab >= 0 {
		line = line[:tab]
	}
	return line, true
}

// gitIgnoredIntentError returns an error wrapping commitqueue.ErrPathIgnored
// naming the first of relPaths that git ignores, or nil when none is ignored.
func gitIgnoredIntentError(repoRoot string, relPaths []string) error {
	for _, rel := range relPaths {
		if rule, ignored := isGitIgnored(repoRoot, rel); ignored {
			return fmt.Errorf("commit-queue: %s is ignored by git rule %s, so this intent can never succeed. %s: %w",
				filepath.ToSlash(rel), rule, wipnoteGitignoreAdvice, commitqueue.ErrPathIgnored)
		}
	}
	return nil
}

// warnIfIntentPathsIgnored prints the ignored-path explanation to w at enqueue
// time so the operator learns about the misconfiguration when the intent is
// recorded, not after five silent retries. The intent is still recorded — the
// artifact state it names is real and the queue is where `check --gate` looks
// for it — so this is advisory only.
func warnIfIntentPathsIgnored(w io.Writer, repoRoot string, relPaths []string) {
	if err := gitIgnoredIntentError(repoRoot, relPaths); err != nil {
		fmt.Fprintf(w, "warning: %v\n", err)
	}
}

// wipnoteIgnoreRules are the repo-level .gitignore lines that ignore the whole
// .wipnote tree — including the work-item artifacts that must be committed.
var wipnoteIgnoreRules = map[string]struct{}{
	".wipnote": {}, ".wipnote/": {}, "/.wipnote": {}, "/.wipnote/": {},
	".wipnote/*": {}, "/.wipnote/*": {}, "**/.wipnote": {}, "**/.wipnote/": {},
	".wipnote/**": {}, "/.wipnote/**": {},
}

// repoGitignoreIgnoresWipnote scans <repoRoot>/.gitignore for a rule that
// ignores the whole .wipnote tree and returns the offending line. A missing or
// unreadable .gitignore is simply "no rule". It deliberately does not consult
// git (init may run before `git init`), so it only sees the repo-level file —
// which is where the over-broad rule lands in practice (GH#172).
func repoGitignoreIgnoresWipnote(repoRoot string) (rule string, found bool) {
	f, err := os.Open(filepath.Join(repoRoot, ".gitignore"))
	if err != nil {
		return "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if _, hit := wipnoteIgnoreRules[line]; hit {
			return line, true
		}
	}
	return "", false
}

// warnIfRepoGitignoreIgnoresWipnote prints the init-time warning for an
// over-broad repo-level .gitignore rule (GH#172). It returns true when a
// warning was printed so callers/tests can assert on it.
func warnIfRepoGitignoreIgnoresWipnote(w io.Writer, repoRoot string) bool {
	rule, found := repoGitignoreIgnoresWipnote(repoRoot)
	if !found {
		return false
	}
	fmt.Fprintf(w, "warning: .gitignore rule %q ignores the whole .wipnote/ tree, so work-item artifacts "+
		"(features/, bugs/, spikes/, tracks/) can never be committed and every deferred artifact commit will be refused.\n  %s\n",
		rule, wipnoteGitignoreAdvice)
	return true
}
