package main

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateProfilesCmd is `wipnote delegate profiles` (feat-8f910039): the
// worker profile table, read from internal/delegate/session so the help cannot
// drift from what spawn does. It replaces the table in the retired delegate
// skill.
func delegateProfilesCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "profiles [--json]",
		Short: "List the worker profiles for delegate spawn --profile, with model, effort and tools",
		Long: `Prints each profile delegate spawn --profile accepts: the built-in ones and any
you set in ~/.wipnote/config.json under delegate.claude.profiles. A profile of a
built-in's name replaces it, and delegate.claude.defaultProfile picks the default.
Pick the lowest effort that suits the task. Add only what the task needs with
--tool, --mcp or --plugin. Claude workers only, except that review also works on
a Codex worker, where it means --sandbox read-only.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg := session.LoadClaudeConfig(paths.UserConfigPath())
			rows := cfg.ProfileTable()
			if asJSON {
				return writeJSON(cmd.OutOrStdout(), rows)
			}
			return printProfileTable(cmd, rows, cfg.DefaultProfile)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print JSON")
	return cmd
}

// printProfileTable writes one aligned line per profile.
func printProfileTable(cmd *cobra.Command, rows []session.ProfileRow, def string) error {
	if def == "" {
		def = session.DefaultProfile
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PROFILE\tMODEL\tEFFORT\tTOOLS\tNOTE")
	for _, r := range rows {
		name := r.Name
		note := r.Note
		if name == def {
			note = strings.TrimSpace("default. " + note)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, dash(r.Model), dash(r.Effort), strings.Join(r.Tools, ","), note)
	}
	return tw.Flush()
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
