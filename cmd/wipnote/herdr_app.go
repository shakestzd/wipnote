package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/herdrapp"
)

// delegateInstallBoardCmd is `wipnote delegate install-board`: the layout that
// shows each agent's work item in the Herdr app.
func delegateInstallBoardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install-board",
		Short: "Copy the wipnote-board layout and mod into the Herdr app's home",
		Long: `Copies the wipnote-board layout and its mods (the board, a pane header and a tab bar that hides settled tabs) into ~/.config/herdr-app, or
$HERDR_APP_HOME when set. Your own layout stays as it is. Switch between them
with ctrl-shift-l. It never overwrites: if any target exists, it writes
nothing. To remove the board, delete the paths it prints.

The mod runs "wipnote delegate list --json" every 5 seconds for each local
space. It reads no other files and makes no network calls.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			home := herdrapp.Home(os.Getenv, userHome)
			wrote, err := herdrapp.Install(home)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			for _, p := range wrote {
				fmt.Fprintln(out, "wrote", p)
			}
			for _, mod := range []string{herdrapp.LayoutName, herdrapp.HeaderMod, herdrapp.TabsMod} {
				fmt.Fprintf(out, "next: herdr-app check %s\n", filepath.Join(home, "mods", mod))
			}
			fmt.Fprintf(out, "then: herdr-app do layout.switch --name %s\n", herdrapp.LayoutName)
			return nil
		},
	}
}
