package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// fileDiff holds per-file diff statistics.
type fileDiff struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

// reviewSummary is the structured result of git diff analysis.
type reviewSummary struct {
	Base         string     `json:"base"`
	FilesChanged int        `json:"files_changed"`
	Insertions   int        `json:"insertions"`
	Deletions    int        `json:"deletions"`
	Files        []fileDiff `json:"files"`
}

func reviewCmd() *cobra.Command {
	var base string
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "review",
		Short: "Structured diff summary against a base branch",
		Long: `Show a structured diff summary of changes against a base branch.

Groups changes by file with per-file +/- line counts and a total summary.
Always exits 0 (informational).

Example:
  wipnote review
  wipnote review --base develop
  wipnote review --json`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runReview(base, jsonOut)
		},
	}

	cmd.Flags().StringVar(&base, "base", "main", "Base branch to diff against")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	return cmd
}

// runReview executes git diff commands and renders the summary.
func runReview(base string, jsonOut bool) error {
	summary, err := buildReviewSummary(base)
	if err != nil {
		return err
	}

	if jsonOut {
		return printReviewJSON(summary)
	}
	printReviewText(summary)
	return nil
}

// buildReviewSummary runs git diff --stat and assembles the structured result.
func buildReviewSummary(base string) (*reviewSummary, error) {
	files, err := parseFileDiffs(base)
	if err != nil {
		return nil, err
	}

	totals, err := parseDiffTotals(base)
	if err != nil {
		return nil, err
	}

	return &reviewSummary{
		Base:         base,
		FilesChanged: totals.filesChanged,
		Insertions:   totals.insertions,
		Deletions:    totals.deletions,
		Files:        files,
	}, nil
}

// diffTotals holds the aggregate numbers from the final --stat line.
type diffTotals struct {
	filesChanged int
	insertions   int
	deletions    int
}

// reStatLine matches the summary line: "3 files changed, 42 insertions(+), 7 deletions(-)"
var reStatLine = regexp.MustCompile(`(\d+) files? changed(?:, (\d+) insertions?\(\+\))?(?:, (\d+) deletions?\(-\))?`)

// parseDiffTotals runs git diff --stat and extracts the summary totals.
func parseDiffTotals(base string) (diffTotals, error) {
	out, err := gitOutput("diff", "--stat", base+"...HEAD")
	if err != nil {
		return diffTotals{}, fmt.Errorf("git diff --stat: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		m := reStatLine.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		t := diffTotals{
			filesChanged: atoi(m[1]),
			insertions:   atoi(m[2]),
			deletions:    atoi(m[3]),
		}
		return t, nil
	}

	// No changes found — return zero totals rather than an error.
	return diffTotals{}, nil
}

// reNumstat matches lines like: "42\t7\tpath/to/file.go"
var reNumstat = regexp.MustCompile(`^(\d+)\t(\d+)\t(.+)$`)

// parseFileDiffs runs git diff --numstat and returns per-file breakdown.
func parseFileDiffs(base string) ([]fileDiff, error) {
	out, err := gitOutput("diff", "--numstat", base+"...HEAD")
	if err != nil {
		return nil, fmt.Errorf("git diff --numstat: %w", err)
	}

	var files []fileDiff
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := reNumstat.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		files = append(files, fileDiff{
			Path:      m[3],
			Additions: atoi(m[1]),
			Deletions: atoi(m[2]),
		})
	}
	return files, nil
}

// printReviewText renders the human-readable review summary.
func printReviewText(s *reviewSummary) {
	fmt.Printf("Review: diff against %q\n\n", s.Base)

	if len(s.Files) == 0 {
		fmt.Println("  No changes detected.")
		return
	}

	for _, f := range s.Files {
		fmt.Printf("  %-60s  +%-4d  -%-4d\n", f.Path, f.Additions, f.Deletions)
	}

	fmt.Printf("\n%d file(s) changed  +%d insertions  -%d deletions\n",
		s.FilesChanged, s.Insertions, s.Deletions)
}

// printReviewJSON marshals the summary as pretty-printed JSON.
func printReviewJSON(s *reviewSummary) error {
	enc, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	fmt.Println(string(enc))
	return nil
}

// gitOutput runs a git sub-command and returns its stdout as a string.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// atoi converts a string to int, returning 0 for empty or invalid input.
func atoi(s string) int {
	if s == "" {
		return 0
	}
	n, _ := strconv.Atoi(s)
	return n
}
