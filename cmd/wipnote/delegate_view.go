package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/spf13/cobra"
)

// delegateViewCmd is `wipnote delegate view <run-id>`: the plain Go viewer
// that runs in a run's pane (it replaces the codex-herdr-pane mod's Textual
// viewer, owner decision q-viewer=go-plain). It follows the run's clean.md
// until the exit file appears, then prints the status and the answer.
func delegateViewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "view <run-id>",
		Short: "Follow a delegated run's log until it ends, then show its answer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			store, err := runstore.New(filepath.Dir(wipnoteDir))
			if err != nil {
				return err
			}
			rf, err := store.Run(args[0])
			if err != nil {
				return fmt.Errorf("delegate view: %w", err)
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return render.View(ctx, rf, cmd.OutOrStdout(), render.ViewOptions{})
		},
	}
}
