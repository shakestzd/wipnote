package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// delegateStopCmd is `wipnote delegate stop NAME`: the verified stop. It
// prints {pane_closed, tab_closed, survivors[], why} and exits 1 when the
// pane stays open.
func delegateStopCmd() *cobra.Command {
	var wait time.Duration
	cmd := &cobra.Command{
		Use:   "stop NAME [--wait D]",
		Short: "Stop an interactive worker and close its pane only at a bare shell",
		Long: `Records the worker's pids from herdr process-info, asks the agent to exit
(only when its input line holds no draft), waits until process-info shows the
shell alone, closes the pane, then checks each recorded pid. It never signals or
kills. The workers tab closes when its last worker stops. Prints
{run, name, pane_closed, tab_closed, survivors, why}; why always says why the
pane stayed open. Exits 1 when the pane was not closed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			d, _, err := delegateSessionDeps(wipnoteDir)
			if err != nil {
				return err
			}
			res, err := session.Stop(cmd.Context(), d, args[0], session.StopOptions{ExitWait: wait})
			if err != nil {
				return err
			}
			if err := writeJSON(cmd.OutOrStdout(), res); err != nil {
				return err
			}
			if !res.PaneClosed {
				return fmt.Errorf("delegate stop: pane not closed: %s", res.Why)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&wait, "wait", session.DefaultExitWait, "how long to wait for the agent to exit")
	return cmd
}
