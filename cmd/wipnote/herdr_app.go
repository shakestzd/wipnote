package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/herdrapp"
)

// delegateInstallLayoutCmd is `wipnote delegate install-layout`: Herdr's main
// layout with each worker listed once, nested under the agent that started it.
func delegateInstallLayoutCmd() *cobra.Command {
	var replace bool
	cmd := &cobra.Command{
		Use:     "install-layout",
		Aliases: []string{"install-board"},
		Short:   "Copy the wipnote-main layout and its mods into the Herdr app's home",
		Long: `Copies the wipnote-main layout and its mods (an agents list that nests each
delegate worker under its caller, and a pane header) into ~/.config/herdr-app, or
$HERDR_APP_HOME when set. Your own layouts stay as they are. Switch between them
with ctrl-shift-l. It refuses when a target exists and writes nothing; --replace
moves the existing files to a dated folder in that home first. To remove the
layout, delete the paths it prints.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			userHome, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			home := herdrapp.Home(os.Getenv, userHome)
			out := cmd.OutOrStdout()
			var wrote []string
			if replace {
				var moved string
				wrote, moved, err = herdrapp.Replace(home, time.Now().Format("20060102-150405"))
				if moved != "" {
					fmt.Fprintln(out, "moved the existing files to", moved)
				}
			} else {
				wrote, err = herdrapp.Install(home)
			}
			if err != nil {
				return err
			}
			for _, p := range wrote {
				fmt.Fprintln(out, "wrote", p)
			}
			for _, mod := range []string{herdrapp.AgentsMod, herdrapp.HeaderMod} {
				fmt.Fprintf(out, "next: herdr-app check %s\n", filepath.Join(home, "mods", mod))
			}
			fmt.Fprintf(out, "then: herdr-app do layout.switch --name %s\n", herdrapp.LayoutName)
			return nil
		},
	}
	cmd.Flags().BoolVar(&replace, "replace", false, "move existing files aside instead of refusing")
	return cmd
}
