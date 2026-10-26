package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

func ciStatusCmd() *cobra.Command {
	var jsonOut bool
	cmd := &cobra.Command{
		Use:   "status [pr|branch]",
		Short: "Show a pull request's CI checks and linked work item",
		Long: "Read a pull request's checks through gh and summarise them. The argument is a PR\n" +
			"number or a head branch; with none, the current branch's PR, else your newest open PR.\n" +
			"A missing or signed-out gh, or any failed gh read, reports not-available and exits 0.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := ""
			if len(args) == 1 {
				arg = args[0]
			}
			return runCIStatus(cmd.Context(), os.Stdout, gitwatch.ExecRunner{}, arg, jsonOut)
		},
	}
	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output the snapshot as JSON")
	return cmd
}

// runCIStatus builds the snapshot for arg and writes it to w.
func runCIStatus(ctx context.Context, w io.Writer, r gitwatch.Runner, arg string, jsonOut bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	snap, err := gitwatch.BuildSnapshot(ctx, r, arg, time.Now)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(w, snap)
	}
	writeCIStatusText(w, snap)
	return nil
}

// writeCIStatusText prints a title line, the counts, and every check that
// is not pass or skipping (already sorted fail first) with its link.
func writeCIStatusText(w io.Writer, s *gitwatch.Snapshot) {
	if !s.Available {
		fmt.Fprintf(w, "CI status not available: %s\n", s.Reason)
		return
	}
	if s.PR == nil {
		fmt.Fprintf(w, "CI status: %s\n", s.Reason)
		return
	}
	fmt.Fprintf(w, "PR #%d %s (%s -> %s, %s)\n", s.PR.Number, s.PR.Title, s.PR.Branch, s.PR.Base, s.PR.State)
	if s.WorkItem != "" {
		fmt.Fprintf(w, "work item: %s\n", s.WorkItem)
	}
	c := s.Counts
	fmt.Fprintf(w, "checks: %d pass, %d fail, %d pending, %d other (%d total)\n",
		c.Pass, c.Fail, c.Pending, c.Other, c.Total)
	for _, ch := range s.Checks {
		if ch.Bucket == gitwatch.BucketPass || ch.Bucket == gitwatch.BucketSkipping {
			continue
		}
		name := ch.Name
		if ch.Workflow != "" {
			name = ch.Workflow + " / " + ch.Name
		}
		fmt.Fprintf(w, "  %-8s %s  %s\n", ch.Bucket, name, ch.Link)
	}
	writeReviewLine(w, s.Review)
	for _, n := range s.Notes {
		fmt.Fprintf(w, "note: %s\n", n)
	}
}

// writeReviewLine prints "review: <decision> · <n> open threads · merge
// <state>", where a mergeability still being computed shows as computing.
func writeReviewLine(w io.Writer, rv *gitwatch.Review) {
	if rv == nil {
		return
	}
	merge := rv.MergeState
	if rv.Mergeable == gitwatch.MergeableComputing {
		merge = gitwatch.MergeableComputing
	}
	line := fmt.Sprintf("review: %s · %d open threads · merge %s", rv.Decision, rv.ThreadsOpen, merge)
	if len(rv.Requested) > 0 {
		line += " · requested " + strings.Join(rv.Requested, ", ")
	}
	fmt.Fprintln(w, line)
}
