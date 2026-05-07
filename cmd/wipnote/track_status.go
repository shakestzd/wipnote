package main

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// commitInfo holds a parsed commit.
type commitInfo struct {
	Hash    string
	Subject string
}

// featureGroup holds commits belonging to a feature.
type featureGroup struct {
	FeatureID string
	Commits   []commitInfo
}

// groupByPrefix parses git log lines and groups them by feat-xxx: or (feat-xxx) prefix.
// Lines without a recognized prefix go into a "" (unattributed) group.
func groupByPrefix(logLines []string) []featureGroup {
	groups := make(map[string][]commitInfo)

	// Regex: match "feat-XXXX:" or "(feat-XXXX)" in subject
	re := regexp.MustCompile(`\b(feat-[a-f0-9]+)[:\)]`)

	for _, line := range logLines {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) < 2 {
			continue
		}
		hash := parts[0]
		subject := parts[1]
		ci := commitInfo{Hash: hash, Subject: subject}

		if m := re.FindStringSubmatch(subject); m != nil {
			groups[m[1]] = append(groups[m[1]], ci)
		} else {
			groups[""] = append(groups[""], ci)
		}
	}

	// Sort groups alphabetically, unattributed last
	var result []featureGroup
	var keys []string
	for k := range groups {
		if k != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		result = append(result, featureGroup{FeatureID: k, Commits: groups[k]})
	}

	if unattr, ok := groups[""]; ok {
		result = append(result, featureGroup{FeatureID: "", Commits: unattr})
	}

	return result
}

// trackStatusCmd returns the "track status" subcommand.
func trackStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <track-id>",
		Short: "Show feature breakdown derived from git history on a track branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runTrackStatus(args[0])
		},
	}
}

// runTrackStatus executes the track status command.
func runTrackStatus(trackID string) error {
	// 1. Get the track branch name: "trk-{trackID}"
	branchName := "trk-" + trackID

	// 2. Check if branch exists
	checkCmd := exec.Command("git", "rev-parse", "--verify", branchName)
	if err := checkCmd.Run(); err != nil {
		return fmt.Errorf("branch %s not found — the track may not have a worktree yet\nRun 'wipnote yolo --track %s' to create the worktree and branch.", branchName, trackID)
	}

	// 3. Get commits on track branch not on main
	logCmd := exec.Command("git", "log", "--oneline", branchName, "--not", "main")
	out, err := logCmd.Output()
	if err != nil {
		return fmt.Errorf("git log failed: %w", err)
	}

	logLines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(logLines) == 0 || (len(logLines) == 1 && logLines[0] == "") {
		fmt.Printf("No commits on branch %s ahead of main.\n", branchName)
		return nil
	}

	// 4. Group by prefix
	groups := groupByPrefix(logLines)

	// 5. Print results
	fmt.Printf("Track: %s (branch: %s)\n", trackID, branchName)
	fmt.Printf("Commits: %d total\n\n", len(logLines))

	for _, g := range groups {
		label := g.FeatureID
		if label == "" {
			label = "(unattributed)"
		}
		fmt.Printf("  %s (%d commits)\n", label, len(g.Commits))
		for _, c := range g.Commits {
			fmt.Printf("    %s %s\n", c.Hash, c.Subject)
		}
	}

	return nil
}
