package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/shakestzd/wipnote/core/prose"
	"github.com/spf13/cobra"
)

const proseCheckHelp = `Check text against the plain-writing rules and print one line per note.

Process rules: every sentence comes from the user, the branch or the code.
Document what was given. Say what you did not check. Post or publish only
when asked. Notes never block: the exit code is 0 unless --strict is set.

Run "wipnote prose rules" for the rule table. Use it on a pull request body,
issue or README before it is posted.`

func proseCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "prose", Short: "Check text against the plain-writing rules"}
	cmd.AddCommand(proseCheckCmd(), proseRulesCmd())
	return cmd
}

func proseCheckCmd() *cobra.Command {
	var strict bool
	var format string
	cmd := &cobra.Command{
		Use:   "check [FILE|-]",
		Short: "Print prose notes for a file or stdin",
		Long:  proseCheckHelp,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			name := "-"
			if len(args) == 1 {
				name = args[0]
			}
			n, err := runProseCheck(name, c.InOrStdin(), c.OutOrStdout(), format)
			if err != nil {
				return err
			}
			if strict && n > 0 {
				return fmt.Errorf("%d prose notes", n)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false, "exit 1 when there are notes")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return cmd
}

func runProseCheck(name string, in io.Reader, out io.Writer, format string) (int, error) {
	var data []byte
	var err error
	if name == "-" {
		data, err = io.ReadAll(in)
	} else {
		data, err = os.ReadFile(name)
	}
	if err != nil {
		return 0, err
	}
	notes := prose.Check(string(data), prose.Options{})
	if format == "json" {
		if notes == nil {
			notes = []prose.Note{}
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return len(notes), enc.Encode(notes)
	}
	if format != "text" {
		return 0, fmt.Errorf("unknown --format %q: use text or json", format)
	}
	for _, n := range notes {
		fmt.Fprintf(out, "%s:%d:%d  %s  %s\n", name, n.Line, n.Col, n.Rule, n.Message)
	}
	return len(notes), nil
}

func proseRulesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rules",
		Short: "Print the prose rule table",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			for _, r := range prose.Rules() {
				fmt.Fprintf(c.OutOrStdout(), "%-16s %-26s %s\n", r.Name, r.Limit, r.Description)
			}
			return nil
		},
	}
}
