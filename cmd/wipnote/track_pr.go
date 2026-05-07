package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

// trackPRCmd returns the "track pr" subcommand.
func trackPRCmd() *cobra.Command {
	var dryRun bool
	var base string
	cmd := &cobra.Command{
		Use:   "pr <track-id>",
		Short: "Create a PR from track branch to main with auto-generated feature summary",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runTrackPR(args[0], base, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print PR body without creating the PR")
	cmd.Flags().StringVar(&base, "base", "main", "Base branch for the PR")
	return cmd
}

// buildPRBody generates a PR body from commit groups and diff stat output.
func buildPRBody(trackID string, groups []featureGroup, diffStat string) string {
	var sb strings.Builder
	sb.WriteString("## Summary\n\n")
	fmt.Fprintf(&sb, "Track: `%s`\n\n", trackID)

	// Feature breakdown
	sb.WriteString("### Features\n\n")
	totalCommits := 0
	for _, g := range groups {
		totalCommits += len(g.Commits)
		label := g.FeatureID
		if label == "" {
			label = "(unattributed)"
		}
		fmt.Fprintf(&sb, "- **%s** (%d commits)\n", label, len(g.Commits))
	}

	// Diff stats
	sb.WriteString("\n### Stats\n\n")
	fmt.Fprintf(&sb, "- %d commits across %d features\n", totalCommits, len(groups))
	if diffStat != "" {
		sb.WriteString("\n```\n")
		sb.WriteString(diffStat)
		sb.WriteString("\n```\n")
	}

	return sb.String()
}

// runTrackPR executes the track pr command.
func runTrackPR(trackID, base string, dryRun bool) error {
	branchName := trackID

	// Check branch exists
	if err := exec.Command("git", "rev-parse", "--verify", branchName).Run(); err != nil {
		return fmt.Errorf("branch %s not found — track branches are created by YOLO mode\nRun 'wipnote yolo --track %s' to create the worktree and branch.", branchName, trackID)
	}

	// Get commits on track branch not on base
	logCmd := exec.Command("git", "log", "--oneline", branchName, "--not", base)
	logOut, err := logCmd.Output()
	if err != nil {
		return fmt.Errorf("git log failed: %w", err)
	}
	logStr := strings.TrimSpace(string(logOut))
	if logStr == "" {
		return fmt.Errorf("no commits on %s ahead of %s", branchName, base)
	}
	logLines := strings.Split(logStr, "\n")

	// Group by feature prefix
	groups := groupByPrefix(logLines)

	// Get diff stat
	diffCmd := exec.Command("git", "diff", "--stat", base+".."+branchName)
	diffOut, _ := diffCmd.Output()
	diffStat := strings.TrimSpace(string(diffOut))

	// Build PR body
	body := buildPRBody(trackID, groups, diffStat)

	// Build title
	title := fmt.Sprintf("feat: %s", trackID)

	if dryRun {
		fmt.Printf("Title: %s\n\n", title)
		fmt.Println(body)
		return nil
	}

	// Create PR using gh CLI
	cmd := exec.Command("gh", "pr", "create",
		"--title", title,
		"--body", body,
		"--base", base,
		"--head", branchName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
