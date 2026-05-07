// Register in main.go: rootCmd.AddCommand(budgetCmd())
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

const (
	budgetAdvisoryFiles = 10
	budgetHardFiles     = 20
	budgetAdvisoryLines = 300
	budgetHardLines     = 600
)

// budgetResult holds the computed budget metrics.
type budgetResult struct {
	FilesChanged    int    `json:"files_changed"`
	LinesAdded      int    `json:"lines_added"`
	FileStatus      string `json:"file_status"`    // "ok", "advisory", "hard"
	LineStatus      string `json:"line_status"`    // "ok", "advisory", "hard"
	OverallStatus   string `json:"overall_status"` // "ok", "advisory", "hard"
	BaseBranch      string `json:"base_branch"`
	CurrentBranch   string `json:"current_branch"`
	NoChanges       bool   `json:"no_changes"`
	NoFeatureBranch bool   `json:"no_feature_branch"`
}

func budgetCmd() *cobra.Command {
	var base string
	var jsonOut bool
	var strict bool

	cmd := &cobra.Command{
		Use:   "budget",
		Short: "Check if current work stays within YOLO mode scope limits",
		Long: `Compare current branch to base branch and report file/line counts against budget limits.

Advisory limits: 10 files, 300 new lines (yellow warning)
Hard limits:     20 files, 600 new lines (red, exit 1)`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runBudget(base, jsonOut, strict)
		},
	}

	cmd.Flags().StringVar(&base, "base", "main", "Base branch to compare against")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")
	cmd.Flags().BoolVar(&strict, "strict", false, "Treat advisory limits as failures (exit 1)")
	return cmd
}

func runBudget(base string, jsonOut bool, strict bool) error {
	result, err := computeBudget(base)
	if err != nil {
		return err
	}

	if jsonOut {
		return printBudgetJSON(result)
	}
	return printBudgetText(result, strict)
}

// computeBudget runs git commands and builds the result.
func computeBudget(base string) (*budgetResult, error) {
	current, err := gitCurrentBranch()
	if err != nil {
		return nil, fmt.Errorf("get current branch: %w", err)
	}

	result := &budgetResult{BaseBranch: base, CurrentBranch: current}

	if current == base {
		result.NoFeatureBranch = true
		return result, nil
	}

	files, lines, err := gitDiffStats(base)
	if err != nil {
		return nil, fmt.Errorf("git diff stats: %w", err)
	}

	if files == 0 && lines == 0 {
		result.NoChanges = true
		return result, nil
	}

	result.FilesChanged = files
	result.LinesAdded = lines
	result.FileStatus = classify(files, budgetAdvisoryFiles, budgetHardFiles)
	result.LineStatus = classify(lines, budgetAdvisoryLines, budgetHardLines)
	result.OverallStatus = worstOf(result.FileStatus, result.LineStatus)
	return result, nil
}

// classify returns "ok", "advisory", or "hard" based on thresholds.
func classify(val, advisory, hard int) string {
	switch {
	case val >= hard:
		return "hard"
	case val >= advisory:
		return "advisory"
	default:
		return "ok"
	}
}

// worstOf returns the more severe of two statuses.
func worstOf(a, b string) string {
	rank := map[string]int{"ok": 0, "advisory": 1, "hard": 2}
	if rank[a] >= rank[b] {
		return a
	}
	return b
}

// gitCurrentBranch returns the current git branch name.
func gitCurrentBranch() (string, error) {
	out, err := runGit("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// gitDiffStats returns files changed and lines added vs base.
func gitDiffStats(base string) (files int, lines int, err error) {
	// Count changed files.
	statOut, err := runGit("diff", "--stat", base+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	files = countChangedFiles(statOut)

	// Count added lines (lines starting with + but not +++).
	diffOut, err := runGit("diff", base+"...HEAD")
	if err != nil {
		return 0, 0, err
	}
	lines = countAddedLines(diffOut)
	return files, lines, nil
}

// countChangedFiles parses the summary line of git diff --stat output.
func countChangedFiles(statOutput string) int {
	lines := strings.Split(statOutput, "\n")
	count := 0
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Each file diff line contains " | " — the summary line does not have this.
		if strings.Contains(trimmed, " | ") || strings.Contains(trimmed, "|") {
			count++
		}
	}
	return count
}

// countAddedLines counts lines beginning with "+" that are not diff headers.
func countAddedLines(diffOutput string) int {
	count := 0
	for _, line := range strings.Split(diffOutput, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
			count++
		}
	}
	return count
}

// runGit executes a git command and returns stdout.
func runGit(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w — %s", strings.Join(args, " "), err, errBuf.String())
	}
	return out.String(), nil
}

// printBudgetText renders human-readable budget output.
func printBudgetText(r *budgetResult, strict bool) error {
	if r.NoFeatureBranch {
		fmt.Printf("No feature branch detected (on %q — no changes to check)\n", r.CurrentBranch)
		return nil
	}
	if r.NoChanges {
		fmt.Printf("No changes yet (branch: %s vs %s)\n", r.CurrentBranch, r.BaseBranch)
		return nil
	}

	label, symbol := statusLabel(r.OverallStatus)
	fmt.Printf("Budget Status: %s %s\n\n", label, symbol)
	fmt.Printf("  Files changed: %s\n",
		formatBudgetLine(r.FilesChanged, budgetAdvisoryFiles, budgetHardFiles))
	fmt.Printf("  New lines:     %s\n\n",
		formatBudgetLine(r.LinesAdded, budgetAdvisoryLines, budgetHardLines))
	fmt.Printf("  Branch: %s vs %s\n", r.CurrentBranch, r.BaseBranch)

	if r.OverallStatus == "hard" || (strict && r.OverallStatus == "advisory") {
		return fmt.Errorf("budget exceeded: see violations above\nUse 'wipnote budget --base <branch>' to check against a different base branch.")
	}
	return nil
}

// formatBudgetLine builds a budget display line like "3 / 10 (advisory) / 20 (hard limit)".
func formatBudgetLine(val, advisory, hard int) string {
	return fmt.Sprintf("%s / %s (advisory) / %s (hard limit)",
		padLeft(strconv.Itoa(val), 4),
		strconv.Itoa(advisory),
		strconv.Itoa(hard))
}

func padLeft(s string, width int) string {
	for len(s) < width {
		s = " " + s
	}
	return s
}

func statusLabel(status string) (string, string) {
	switch status {
	case "hard":
		return "OVER LIMIT", "x"
	case "advisory":
		return "WARNING", "!"
	default:
		return "OK", "✓"
	}
}

// printBudgetJSON writes the result as JSON.
func printBudgetJSON(r *budgetResult) error {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
