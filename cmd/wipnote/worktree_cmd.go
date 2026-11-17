package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/worktree"
	"github.com/spf13/cobra"
)

// Register in root_commands.go: worktreeCmd()
func worktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "List and clean up the repo's git worktrees",
	}
	cmd.AddCommand(worktreeListCmd(), worktreeCleanCmd())
	return cmd
}

func worktreeRoots() (root, cwd string, err error) {
	if cwd, err = os.Getwd(); err != nil {
		return "", "", err
	}
	if root = paths.MainRepoRoot(cwd); root == "" {
		return "", "", fmt.Errorf("not inside a git repository")
	}
	return root, cwd, nil
}

func worktreeListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show every worktree with its branch, dirty state and merged state",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			root, cwd, err := worktreeRoots()
			if err != nil {
				return err
			}
			entries, err := worktree.ListWorktrees(root, cwd)
			if err != nil {
				return err
			}
			if asJSON {
				return writeWorktreeJSON(c.OutOrStdout(), entries)
			}
			printWorktreeTable(c.OutOrStdout(), entries)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
}

func worktreeCleanCmd() *cobra.Command {
	var yes, asJSON bool
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove clean, merged worktrees and their local branches (dry run unless --yes)",
		Long: `Remove worktrees whose branch is merged upstream (ancestor of the default
branch, squash-merged, or a merged GitHub PR with the same head) and that have
no tracked changes or untracked files. The main checkout, the current worktree
and locked worktrees are kept. Remote branches are never deleted.`,
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			root, cwd, err := worktreeRoots()
			if err != nil {
				return err
			}
			entries, err := worktree.CleanWorktrees(root, cwd, yes)
			if err != nil {
				return err
			}
			if asJSON {
				return writeWorktreeJSON(c.OutOrStdout(), entries)
			}
			printCleanReport(c.OutOrStdout(), entries, yes)
			return nil
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Remove worktrees (default is a dry run)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "Output JSON")
	return cmd
}

func writeWorktreeJSON(w io.Writer, entries []worktree.Entry) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

func printWorktreeTable(w io.Writer, entries []worktree.Entry) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PATH\tBRANCH\tDIRTY\tLOCKED\tMERGED\tWORK ITEM\tMARK")
	for _, e := range entries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.RelPath, orDash(e.Branch),
			dirtyLabel(e), yesNo(e.Locked), e.Merged, orDash(e.WorkItem), markLabel(e))
	}
	tw.Flush()
}

func printCleanReport(w io.Writer, entries []worktree.Entry, applied bool) {
	verb := "Would remove"
	if applied {
		verb = "Removed"
	}
	removable := 0
	for _, e := range entries {
		if e.Removable || e.Action == "removed" || e.Action == "removed (branch kept: delete failed)" {
			removable++
			fmt.Fprintf(w, "%s: %s (%s) - %s\n", verb, e.RelPath, orDash(e.Branch), e.Reason)
		}
	}
	for _, e := range entries {
		if !e.Removable && e.Action == "" {
			fmt.Fprintf(w, "Kept: %s (%s) - %s\n", e.RelPath, orDash(e.Branch), e.Reason)
		}
	}
	if !applied && removable > 0 {
		fmt.Fprintln(w, "\nDry run. Re-run with --yes to remove.")
	}
}

func dirtyLabel(e worktree.Entry) string {
	switch {
	case e.Tracked && e.Untracked:
		return "tracked+untracked"
	case e.Tracked:
		return "tracked"
	case e.Untracked:
		return "untracked"
	}
	return "clean"
}

func markLabel(e worktree.Entry) string {
	switch {
	case e.Main && e.Current:
		return "main, current"
	case e.Main:
		return "main"
	case e.Current:
		return "current"
	}
	return ""
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
