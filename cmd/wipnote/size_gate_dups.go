package main

// Duplication evidence for the size gate and `wipnote review` (GitHub #312).
// DRY is about knowledge, not text, so a repeated block is never a failure:
// it is evidence for a reviewer, who decides whether the two copies encode one
// rule that must change together. The scan is cheap: it fingerprints windows
// of significant lines that this change added and looks for the same window
// anywhere else in the tracked code of the same file types.

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const (
	dupWindow      = 6    // significant lines per fingerprinted window
	dupMinDistinct = 4    // distinct lines a window needs, to skip boilerplate runs
	dupMaxPairs    = 10   // pairs reported, most-repeated first
	dupMaxCorpus   = 5000 // files scanned before the corpus narrows to the change
)

// dupPair is one added block that repeats a block at another location.
type dupPair struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	OtherPath string `json:"other_path"`
	OtherLine int    `json:"other_line"`
	Lines     int    `json:"lines"` // approximate repeated length
}

type dupLoc struct {
	path string
	line int
}

// sigLine is a significant (non-blank, non-comment, non-trivial) line.
type sigLine struct {
	text string
	line int
}

// scanDupEvidence returns added blocks that also appear elsewhere. It never
// fails: any git or read error just means less evidence.
func scanDupEvidence(repoRoot, base string) []dupPair {
	files, err := changedCodeFiles(repoRoot, base)
	if err != nil || len(files) == 0 {
		return nil
	}
	added := addedLineRanges(repoRoot, base)
	newWins := map[string][]dupLoc{}
	for _, rel := range files {
		if isTestSource(rel) {
			continue
		}
		for key, loc := range fileWindows(repoRoot, rel) {
			if added.covers(rel, loc.line, key) {
				newWins[key.text] = append(newWins[key.text], loc)
			}
		}
	}
	if len(newWins) == 0 {
		return nil
	}
	return pairDupWindows(newWins, corpusMatches(repoRoot, files, newWins))
}

// winKey identifies a window by its text and the original line of its last line.
type winKey struct {
	text    string
	endLine int
}

// fileWindows maps each qualifying window in rel to its starting location.
func fileWindows(repoRoot, rel string) map[winKey]dupLoc {
	data, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil || isGeneratedSource(data) {
		return nil
	}
	sig := significantLines(splitSourceLines(string(data)))
	out := map[winKey]dupLoc{}
	for i := 0; i+dupWindow <= len(sig); i++ {
		if text, ok := windowText(sig[i : i+dupWindow]); ok {
			out[winKey{text, sig[i+dupWindow-1].line}] = dupLoc{rel, sig[i].line}
		}
	}
	return out
}

func windowText(win []sigLine) (string, bool) {
	distinct := map[string]bool{}
	parts := make([]string, len(win))
	for i, s := range win {
		distinct[s.text] = true
		parts[i] = s.text
	}
	return strings.Join(parts, "\n"), len(distinct) >= dupMinDistinct
}

// significantLines drops blank, comment, trivial and import lines: package
// headers and import lists repeat across every file without sharing a rule.
func significantLines(lines []string) []sigLine {
	var out []sigLine
	inImports := false
	for i, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case inImports:
			inImports = t != ")"
			continue
		case t == "import (":
			inImports = true
			continue
		case len(t) < 4 || isImportOrComment(t):
			continue
		}
		out = append(out, sigLine{t, i + 1})
	}
	return out
}

func isImportOrComment(t string) bool {
	for _, p := range []string{"//", "#", "*", "/*", "package ", "import ", "from "} {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// corpusMatches finds every location of each new window across tracked and
// changed files of the same types.
func corpusMatches(repoRoot string, changed []string, newWins map[string][]dupLoc) map[string][]dupLoc {
	corpus := corpusFiles(repoRoot, changed)
	found := map[string][]dupLoc{}
	for _, rel := range corpus {
		for key, loc := range fileWindows(repoRoot, rel) {
			if _, ok := newWins[key.text]; ok {
				found[key.text] = append(found[key.text], loc)
			}
		}
	}
	return found
}

func corpusFiles(repoRoot string, changed []string) []string {
	exts := map[string]bool{}
	for _, rel := range changed {
		exts[strings.ToLower(filepath.Ext(rel))] = true
	}
	tracked, _ := sizeGit(repoRoot, "ls-files")
	seen := map[string]bool{}
	var out []string
	for _, rel := range append(strings.Split(tracked, "\n"), changed...) {
		if rel == "" || seen[rel] || !exts[strings.ToLower(filepath.Ext(rel))] || sizeSkipPath(rel) || isTestSource(rel) {
			continue
		}
		seen[rel] = true
		out = append(out, rel)
	}
	if len(out) > dupMaxCorpus {
		return changed
	}
	return out
}

type dupPairKey struct{ a, b string }

// pairDupWindows collapses window matches into one pair per (new file, other
// file), keeping the first location and counting the overlapping windows.
func pairDupWindows(newWins, found map[string][]dupLoc) []dupPair {
	pairs := map[dupPairKey]*dupPair{}
	for text, news := range newWins {
		for _, n := range news {
			for _, o := range found[text] {
				if o != n {
					addDupMatch(pairs, n, o)
				}
			}
		}
	}
	return sortDupPairs(pairs)
}

func addDupMatch(pairs map[dupPairKey]*dupPair, n, o dupLoc) {
	k := dupPairKey{n.path, o.path}
	p, ok := pairs[k]
	if !ok {
		pairs[k] = &dupPair{Path: n.path, Line: n.line, OtherPath: o.path, OtherLine: o.line, Lines: dupWindow}
		return
	}
	p.Lines++
	if n.line < p.Line {
		p.Line, p.OtherLine = n.line, o.line
	}
}

func sortDupPairs(pairs map[dupPairKey]*dupPair) []dupPair {
	out := make([]dupPair, 0, len(pairs))
	for k, p := range pairs {
		// Two new copies of one block are one finding, not two.
		if _, rev := pairs[dupPairKey{k.b, k.a}]; rev && k.a > k.b {
			continue
		}
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Lines != out[j].Lines {
			return out[i].Lines > out[j].Lines
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Line < out[j].Line
	})
	return out[:min(len(out), dupMaxPairs)]
}

func isTestSource(rel string) bool {
	base := filepath.Base(rel)
	return strings.HasSuffix(base, "_test.go") || strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.py") || strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

// addedRanges holds, per file, the line ranges this change added. A file
// with no entry and allAdded set was untracked: every line is new.
type addedRanges struct {
	ranges   map[string][][2]int
	allAdded map[string]bool
}

// covers reports whether any line of the window ending at key.endLine and
// starting at start was added by this change.
func (a addedRanges) covers(rel string, start int, key winKey) bool {
	if a.allAdded[rel] {
		return true
	}
	for _, r := range a.ranges[rel] {
		if r[0] <= key.endLine && start <= r[1] {
			return true
		}
	}
	return false
}

var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

func addedLineRanges(repoRoot, base string) addedRanges {
	a := addedRanges{ranges: map[string][][2]int{}, allAdded: map[string]bool{}}
	untracked, _ := sizeGit(repoRoot, "ls-files", "--others", "--exclude-standard")
	for _, rel := range strings.Split(untracked, "\n") {
		a.allAdded[rel] = rel != ""
	}
	diff, _ := sizeGit(repoRoot, "diff", "-U0", "--no-color", "--no-renames", base)
	file := ""
	for _, line := range strings.Split(diff, "\n") {
		if rest, ok := strings.CutPrefix(line, "+++ b/"); ok {
			file = rest
			continue
		}
		m := hunkHeader.FindStringSubmatch(line)
		if m == nil || file == "" {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		count := 1
		if m[2] != "" {
			count, _ = strconv.Atoi(m[2])
		}
		if count > 0 {
			a.ranges[file] = append(a.ranges[file], [2]int{start, start + count - 1})
		}
	}
	return a
}
