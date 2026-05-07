package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func spikeResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset <spike-id>",
		Short: "Reset an in-progress spike back to todo",
		Long: `Reset an in-progress spike by setting its status back to 'todo'.
This is a non-destructive operation — all history, steps, edges, and description
are preserved. The agent assignment is cleared.

Errors if the spike is not currently in-progress.

Example:
  wipnote spike reset spk-a1b2c3d4`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			title, err := executeReset("spike", args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Reset: %s  %s\n", args[0], title)
			return nil
		},
	}
}
