package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
)

func pluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Manage the wipnote Claude Code plugin",
	}
	cmd.AddCommand(pluginInstallCmd())
	cmd.AddCommand(pluginBuildPortsCmd())
	return cmd
}

func pluginInstallCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Install the wipnote plugin for Claude Code",
		Long:  "Installs the wipnote plugin into Claude Code via 'claude plugin install wipnote'.",
		RunE: func(cmd *cobra.Command, args []string) error {
			claudePath, err := exec.LookPath("claude")
			if err != nil {
				return fmt.Errorf("'claude' not found on PATH: install Claude Code first (https://claude.ai/code)")
			}

			// Register the marketplace so Claude Code can find the plugin.
			fmt.Fprintln(cmd.OutOrStdout(), "Registering wipnote marketplace...")
			_ = exec.Command(claudePath, "plugin", "marketplace", "add", wipnoteMarketplaceRepo).Run()

			fmt.Fprintln(cmd.OutOrStdout(), "Installing plugin...")
			c := exec.Command(claudePath, "plugin", "install", "wipnote@wipnote")
			c.Stdout = os.Stdout
			c.Stderr = os.Stderr
			if err := c.Run(); err != nil {
				return fmt.Errorf("claude plugin install wipnote@wipnote: %w", err)
			}

			fmt.Fprintln(cmd.OutOrStdout(), "Plugin installed successfully.")
			return nil
		},
	}
}
