package main

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateListCmd is `wipnote delegate list`: runs with worker readiness.
func delegateListCmd() *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "list [--all] [--json]",
		Short: "List delegation runs and worker readiness",
		Long: `Lists the open runs (every run with --all). An interactive worker's state is
one of starting, ready, working, question, done, idle_no_result, idle_shell,
blocked (a folder-trust dialog, named) and gone; a one-shot or ended run's is
running, ended, failed (a non-zero exit) or gone. GROUP is the state's Projects
Overview group: Working, Waiting on you, Ready for review, Landing, Idle or
Resolved. list also renames and pins a worker's herdr agent once it appears,
records bound sessions in the ledger, and keeps each worker's state in its run
folder (state.json) for the orchestra band and tree.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			d, _, err := delegateSessionDeps(wipnoteDir)
			if err != nil {
				return err
			}
			rows, err := session.List(cmd.Context(), d, all)
			if err != nil {
				return err
			}
			if asJSON {
				if rows == nil {
					rows = []session.Row{}
				}
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return printRows(cmd, rows)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include ended runs")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

func printRows(cmd *cobra.Command, rows []session.Row) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "no open delegation runs")
		return err
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "RUN\tNAME\tHARNESS\tWORK ITEM\tPANE\tGROUP\tSTATE\tDETAIL")
	for _, r := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", r.Run, r.Name, r.Harness, r.WorkItem, r.Pane, r.Group.Label(), r.State, r.Detail)
	}
	return tw.Flush()
}

// delegateTreeCmd is `wipnote delegate tree`: caller, runs, worker sessions
// and sub-agents.
func delegateTreeCmd() *cobra.Command {
	var all, asJSON bool
	cmd := &cobra.Command{
		Use:   "tree [--all] [--json]",
		Short: "Show caller sessions, their runs, worker sessions and sub-agents",
		Long: `Shows the current session's runs as a tree (every caller with --all, or when no
session is known): caller, run, worker session (a /clear adds one), sub-agent.
Each run shows its state and Overview group; an open worker's state is the one
the last list observed (tree itself asks herdr nothing).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			d, _, err := delegateSessionDeps(wipnoteDir)
			if err != nil {
				return err
			}
			caller := ""
			if !all {
				caller = hooks.EnvSessionID("")
			}
			nodes, err := session.Tree(d, caller)
			if err != nil {
				return err
			}
			if asJSON {
				if nodes == nil {
					nodes = []session.CallerNode{}
				}
				return writeJSON(cmd.OutOrStdout(), nodes)
			}
			session.RenderTree(cmd.OutOrStdout(), nodes)
			return nil
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "every caller session")
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}
