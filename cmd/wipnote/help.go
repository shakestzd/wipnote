package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// renderCompactHelp walks the cobra command tree rooted at root and produces a
// compact, agent-friendly CLI reference. It iterates cobra Groups in
// registration order and renders member commands alphabetically within each
// group. Commands with no GroupID are omitted (treated as internal plumbing).
// The function targets a ~30-line output budget.
//
// Ordering and grouping are derived entirely from cobra Group metadata set at
// command registration time — there are no hand-maintained name slices here.
func renderCompactHelp(root *cobra.Command) string {
	// Build a lookup from command name to cobra.Command for visible top-level cmds.
	cmdMap := make(map[string]*cobra.Command)
	for _, c := range root.Commands() {
		if c.Hidden || c.Deprecated != "" {
			continue
		}
		cmdMap[c.Name()] = c
	}

	var lines []string
	lines = append(lines, "## CLI Quick Reference")
	lines = append(lines, "wipnote CLI commands:")

	// Iterate groups in registration order (cobra preserves insertion order).
	for _, grp := range root.Groups() {
		// Collect member commands for this group, sorted alphabetically.
		var members []*cobra.Command
		for _, c := range cmdMap {
			if c.GroupID == grp.ID {
				members = append(members, c)
			}
		}
		if len(members) == 0 {
			continue
		}
		sort.Slice(members, func(i, j int) bool {
			return members[i].Name() < members[j].Name()
		})

		lines = append(lines, grp.Title+":")
		for _, c := range members {
			subNames := collectSubcommandNames(c)
			var line string
			if len(subNames) > 0 {
				line = fmt.Sprintf("  %-14s [%s] — %s", c.Name(), strings.Join(subNames, "|"), c.Short)
			} else {
				line = fmt.Sprintf("  %-14s %s", c.Name(), c.Short)
			}
			lines = append(lines, line)
		}
	}

	lines = append(lines, `Required flags: feature/bug create require --track <id> --description "…"`)
	lines = append(lines, "Run `wipnote help --compact` for this reference on demand.")

	return strings.Join(lines, "\n")
}

// collectSubcommandNames returns sorted, non-hidden, non-deprecated
// subcommand names for cmd, up to depth 1.
func collectSubcommandNames(cmd *cobra.Command) []string {
	var names []string
	for _, sub := range cmd.Commands() {
		if sub.Hidden || sub.Deprecated != "" {
			continue
		}
		names = append(names, sub.Name())
	}
	sort.Strings(names)
	return names
}

// helpCmd returns the "wipnote help" command with --compact flag support.
func helpCmd() *cobra.Command {
	var compact bool

	cmd := &cobra.Command{
		Use:   "help",
		Short: "Show help or compact CLI reference for LLM context",
		Long:  "Show full help or --compact one-line-per-command reference for injecting into LLM context.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if compact {
				fmt.Println(renderCompactHelp(cmd.Root()))
				return nil
			}
			// Default: show root help via parent
			return cmd.Parent().Help()
		},
	}

	cmd.Flags().BoolVar(&compact, "compact", false, "Output a concise per-command reference for LLM context injection")
	return cmd
}
