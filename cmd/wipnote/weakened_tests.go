package main

// Evidence that a change may have weakened its own checks (feat-a1daeef8).
// An agent can turn a gate green by deleting a test or skipping one, and
// nothing in review or check showed it. This reads a unified diff and counts
// removed test functions and added skips, and lists edits to CI, lint and
// guard config. The lines are evidence only: a correct refactor removes tests
// too, so nothing here changes an exit code.

import (
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

// evidenceBlock is the evidence printed by `wipnote review` and `check --gate`.
type evidenceBlock struct {
	RemovedTests int      `json:"removed_tests"`
	AddedSkips   int      `json:"added_skips"`
	ConfigEdits  []string `json:"config_edits"`
	AddedLines   int      `json:"added_lines"`
}

// testSpec holds the patterns for one language's test declarations and skips.
type testSpec struct {
	decl *regexp.Regexp // capture 1 is the test's name
	skip *regexp.Regexp
}

var (
	jsDecl = regexp.MustCompile(`^\s*(?:x?it|x?test)(?:\.(?:skip|only))?\(\s*['"` + "`" + `]([^'"` + "`" + `]+)['"` + "`" + `]`)
	jsSkip = regexp.MustCompile(`\b(?:it|test|describe)\.skip\(|\bx(?:it|test|describe)\(`)
)

var testSpecs = map[string]testSpec{
	".go": {
		decl: regexp.MustCompile(`^func (Test\w+)\(`),
		skip: regexp.MustCompile(`\bt\.Skip(?:f|Now)?\(`),
	},
	".js":  {decl: jsDecl, skip: jsSkip},
	".jsx": {decl: jsDecl, skip: jsSkip},
	".ts":  {decl: jsDecl, skip: jsSkip},
	".tsx": {decl: jsDecl, skip: jsSkip},
	".py": {
		decl: regexp.MustCompile(`^\s*(?:async )?def (test\w*)\(`),
		skip: regexp.MustCompile(`@(?:pytest\.mark|unittest)\.skip|\bpytest\.skip\(`),
	},
}

// configPatterns match repo-relative paths of CI, lint and guard config.
var configPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^\.github/workflows/`),
	regexp.MustCompile(`^\.gitlab-ci\.ya?ml$`),
	regexp.MustCompile(`(^|/)\.golangci\.ya?ml$`),
	regexp.MustCompile(`(^|/)\.?ruff\.toml$`),
	regexp.MustCompile(`(^|/)\.eslintrc[^/]*$`),
	regexp.MustCompile(`(^|/)eslint\.config\.[cm]?js$`),
	regexp.MustCompile(`(^|/)\.pre-commit-config\.ya?ml$`),
	regexp.MustCompile(`^\.githooks/`),
	regexp.MustCompile(`^\.wipnote/guard-profile\.yaml$`),
	regexp.MustCompile(`^plugin/hooks/`),
}

func isConfigPath(p string) bool {
	for _, re := range configPatterns {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// addConfigEdit lists p once when it is CI, lint or guard config. A deleted
// config file reaches here from its "---" line.
func addConfigEdit(ev *evidenceBlock, p string) {
	if !isConfigPath(p) {
		return
	}
	for _, have := range ev.ConfigEdits {
		if have == p {
			return
		}
	}
	ev.ConfigEdits = append(ev.ConfigEdits, p)
}

// parseEvidenceDiff reads a unified diff and returns its evidence. It needs no
// git, so tests feed it literal diffs. A test removed and added again under the
// same name in one file (a changed signature) is not a removal. AddedLines is
// left for the caller, which gets it from git's own totals.
func parseEvidenceDiff(diff string) evidenceBlock {
	var ev evidenceBlock
	var file string
	var spec testSpec
	var hasSpec bool
	removed, added := map[string]int{}, map[string]int{}

	flush := func() {
		for name, n := range removed {
			if n > added[name] {
				ev.RemovedTests += n - added[name]
			}
		}
		removed, added = map[string]int{}, map[string]int{}
	}
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "+++ "):
			// A deleted file has "+++ /dev/null": keep the spec its "---" line set.
			file = strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/")
			if file == "/dev/null" {
				continue
			}
			flush()
			spec, hasSpec = testSpecs[strings.ToLower(path.Ext(file))]
			addConfigEdit(&ev, file)
		case strings.HasPrefix(line, "--- "):
			if rest := strings.TrimPrefix(line, "--- "); rest != "/dev/null" {
				flush()
				file = strings.TrimPrefix(rest, "a/")
				spec, hasSpec = testSpecs[strings.ToLower(path.Ext(file))]
				addConfigEdit(&ev, file)
			}
		case hasSpec && strings.HasPrefix(line, "-"):
			if m := spec.decl.FindStringSubmatch(line[1:]); m != nil {
				removed[m[1]]++
			}
		case hasSpec && strings.HasPrefix(line, "+"):
			if m := spec.decl.FindStringSubmatch(line[1:]); m != nil {
				added[m[1]]++
			}
			if spec.skip.MatchString(line[1:]) {
				ev.AddedSkips++
			}
		}
	}
	flush()
	return ev
}

// scanEvidence measures the diff from base to the working tree, uncommitted
// work included. A failed git call yields an empty block: this is advisory.
func scanEvidence(repoRoot, base string) evidenceBlock {
	if base == "" {
		return evidenceBlock{}
	}
	diff, err := sizeGit(repoRoot, "diff", "--no-color", "--no-ext-diff", "--no-renames", base)
	if err != nil {
		return evidenceBlock{}
	}
	ev := parseEvidenceDiff(diff)
	if stat, err := sizeGit(repoRoot, "diff", "--numstat", "--no-renames", base); err == nil {
		for _, l := range strings.Split(stat, "\n") {
			if m := reNumstat.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
				ev.AddedLines += atoi(m[1])
			}
		}
	}
	return ev
}

// writeEvidence prints the evidence block.
func writeEvidence(w io.Writer, ev evidenceBlock) {
	fmt.Fprintln(w, "\nEvidence (informational, never fails the command):")
	fmt.Fprintf(w, "  removed tests: %d\n  added skips: %d\n  added lines: %d\n", ev.RemovedTests, ev.AddedSkips, ev.AddedLines)
	if len(ev.ConfigEdits) == 0 {
		fmt.Fprintln(w, "  config edits: none")
		return
	}
	fmt.Fprintln(w, "  config edits:")
	for _, p := range ev.ConfigEdits {
		fmt.Fprintf(w, "    %s\n", p)
	}
}

// writeGateEvidence is the `check --gate` entry. It returns nothing on purpose:
// the evidence must never change the gate's exit code.
func writeGateEvidence(w io.Writer, repoRoot string) {
	writeEvidence(w, scanEvidence(repoRoot, resolveSizeBase(repoRoot, "")))
}
