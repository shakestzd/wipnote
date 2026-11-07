package main

// The changed-file size gate (GitHub #312). Size limits are review triggers,
// not measures: code over a trigger may stay whole when a one-line reason is
// recorded next to it. The gate looks only at code this change touched, and
// only blocks a trigger the change itself crossed; legacy code that was
// already over the trigger at base is reported, never blocked.

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/hooks"
)

// sizeCodeExts are the file types whose module size is checked. Function
// length is checked where sizeFuncSpans has a scanner.
var sizeCodeExts = map[string]bool{
	".go": true, ".py": true, ".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".rs": true,
}

var sizeFuncSpans = map[string]func([]string) []funcSpan{
	".go": goFuncSpans,
	".py": pyFuncSpans,
}

// sizeFinding is one module or function over its review trigger.
type sizeFinding struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"` // function key; empty for the module
	Line      int    `json:"line,omitempty"`
	Lines     int    `json:"lines"`
	Trigger   int    `json:"trigger"`
	BaseLines int    `json:"base_lines,omitempty"` // 0 when new at base
	Legacy    bool   `json:"legacy,omitempty"`     // already over the trigger at base
	Reason    string `json:"reason,omitempty"`     // recorded with the size-ok marker
}

type sizeReport struct {
	Base     string        `json:"base"`
	Findings []sizeFinding `json:"findings"`
}

// blocking returns the findings this change introduced with no reason.
func (r sizeReport) blocking() []sizeFinding {
	var out []sizeFinding
	for _, f := range r.Findings {
		if !f.Legacy && f.Reason == "" {
			out = append(out, f)
		}
	}
	return out
}

// checkChangedCodeSize is the `wipnote check --gate` entry point. Outside a
// git repository there is no change to scope to, so it does nothing.
func checkChangedCodeSize(w io.Writer, repoRoot string) error {
	base := resolveSizeBase(repoRoot, "")
	if base == "" {
		return nil
	}
	report, err := scanChangedSize(repoRoot, base)
	if err != nil {
		fmt.Fprintf(w, "size gate: skipped (%v)\n", err)
		return nil
	}
	writeSizeReport(w, report)
	writeDupEvidence(w, scanDupEvidence(repoRoot, base))
	if n := len(report.blocking()); n > 0 {
		return fmt.Errorf("size gate: %d changed unit(s) crossed a review trigger with no recorded reason — split them, or keep them whole with a `%s: <reason>` comment", n, hooks.SizeOKMarker)
	}
	return nil
}

// resolveSizeBase returns the commit the change is measured from: the merge
// base of HEAD with branch (or with main/master when branch is empty). On the
// base branch itself that is HEAD, so only uncommitted work is in scope.
// Returns "" when repoRoot is not a git work tree.
func resolveSizeBase(repoRoot, branch string) string {
	if _, err := sizeGit(repoRoot, "rev-parse", "--verify", "--quiet", "HEAD"); err != nil {
		return ""
	}
	candidates := []string{branch}
	if branch == "" {
		candidates = []string{"main", "master", "origin/main", "origin/master"}
	}
	for _, c := range candidates {
		if base, err := sizeGit(repoRoot, "merge-base", "HEAD", c); err == nil && base != "" {
			return base
		}
	}
	head, _ := sizeGit(repoRoot, "rev-parse", "HEAD")
	return head
}

// changedCodeFiles lists code files added or modified since base, including
// uncommitted and untracked ones, as repo-relative paths.
func changedCodeFiles(repoRoot, base string) ([]string, error) {
	diff, err := sizeGit(repoRoot, "diff", "--name-only", "--no-renames", "--diff-filter=AM", base)
	if err != nil {
		return nil, err
	}
	untracked, err := sizeGit(repoRoot, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var files []string
	for _, p := range strings.Split(diff+"\n"+untracked, "\n") {
		if p != "" && !seen[p] && sizeCodeExts[strings.ToLower(filepath.Ext(p))] && !sizeSkipPath(p) {
			seen[p] = true
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

// sizeSkipPath excludes vendored and installed third-party trees.
func sizeSkipPath(p string) bool {
	return strings.Contains("/"+p, "/vendor/") || strings.Contains("/"+p, "/node_modules/")
}

func scanChangedSize(repoRoot, base string) (sizeReport, error) {
	report := sizeReport{Base: base}
	files, err := changedCodeFiles(repoRoot, base)
	if err != nil {
		return report, err
	}
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(repoRoot, rel))
		if err != nil || isGeneratedSource(data) {
			continue
		}
		baseData, baseErr := sizeGit(repoRoot, "show", base+":"+rel)
		var baseLines []string
		if baseErr == nil {
			baseLines = splitSourceLines(baseData)
		}
		cur := splitSourceLines(string(data))
		report.Findings = append(report.Findings, sizeFindingsForFile(rel, cur, baseLines, baseErr == nil)...)
	}
	return report, nil
}

// sizeFindingsForFile compares one file with its base version. A legacy
// finding whose size did not grow is not reported at all.
func sizeFindingsForFile(rel string, cur, base []string, hasBase bool) []sizeFinding {
	var out []sizeFinding
	if len(cur) > hooks.ModuleLimitLines {
		f := sizeFinding{Path: rel, Lines: len(cur), Trigger: hooks.ModuleLimitLines, Reason: fileSizeReason(cur)}
		if hasBase {
			f.BaseLines, f.Legacy = len(base), len(base) > hooks.ModuleLimitLines
		}
		out = appendIfReported(out, f)
	}
	spans, ok := sizeFuncSpans[strings.ToLower(filepath.Ext(rel))]
	if !ok {
		return out
	}
	baseMax := map[string]int{}
	for _, s := range spans(base) {
		baseMax[s.Key] = max(baseMax[s.Key], s.Lines)
	}
	for _, s := range spans(cur) {
		if s.Lines <= hooks.FuncLimitLines {
			continue
		}
		b := baseMax[s.Key]
		out = appendIfReported(out, sizeFinding{Path: rel, Name: s.Key, Line: s.Line, Lines: s.Lines,
			Trigger: hooks.FuncLimitLines, BaseLines: b, Legacy: b > hooks.FuncLimitLines,
			Reason: markerReasonAbove(cur, s.Line-1)})
	}
	return out
}

func appendIfReported(out []sizeFinding, f sizeFinding) []sizeFinding {
	if f.Legacy && f.Lines <= f.BaseLines {
		return out // untouched legacy size: not this change's concern
	}
	return append(out, f)
}

// fileSizeReason returns the reason recorded in the file's leading lines.
func fileSizeReason(lines []string) string {
	for i := 0; i < len(lines) && i < hooks.SizeOKFileWindow; i++ {
		if r := markerReason(lines[i]); r != "" {
			return r
		}
	}
	return ""
}

// markerReasonAbove returns the reason on the declaration at idx or in the
// comment and decorator lines directly above it.
func markerReasonAbove(lines []string, idx int) string {
	if r := markerReason(lines[idx]); r != "" {
		return r
	}
	for i := idx - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(t, "//") && !strings.HasPrefix(t, "#") && !strings.HasPrefix(t, "@") {
			return ""
		}
		if r := markerReason(t); r != "" {
			return r
		}
	}
	return ""
}

// markerReason returns the text after "wipnote:size-ok:", or "" when the
// line has no marker or the marker has no reason.
func markerReason(line string) string {
	_, after, found := strings.Cut(line, hooks.SizeOKMarker)
	if !found {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(after), ":"))
}

func isGeneratedSource(data []byte) bool {
	head := string(data[:min(len(data), 512)])
	return strings.Contains(head, "Code generated") && strings.Contains(head, "DO NOT EDIT")
}

func splitSourceLines(s string) []string {
	s = strings.TrimRight(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func sizeGit(repoRoot string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).Output()
	return strings.TrimRight(string(out), "\n"), err
}
