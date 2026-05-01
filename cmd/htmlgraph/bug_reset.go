package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func bugResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <bug-id>",
		Short: "Reset an in-progress bug back to todo",
		Long: `Reset an in-progress bug by setting its status back to 'todo'.
This is a non-destructive operation — all history, steps, edges, and description
are preserved. The agent assignment is cleared.

Errors if the bug is not currently in-progress.

Example:
  htmlgraph bug reset bug-a1b2c3d4`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title, err := executeReset("bug", args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Reset: %s  %s\n", args[0], title)
			return nil
		},
	}
}
