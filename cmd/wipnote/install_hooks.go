package main

// Register in main.go: rootCmd.AddCommand(installHooksCmd())

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/spf13/cobra"
)

func installHooksCmd() *cobra.Command {
	var uninstall bool
	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Configure git to use .githooks directory",
		RunE: func(_ *cobra.Command, _ []string) error {
			if uninstall {
				return runUninstallHooks()
			}
			return runInstallHooks()
		},
	}
	cmd.Flags().BoolVar(&uninstall, "uninstall", false, "Remove the git hooks path configuration")
	return cmd
}

func runInstallHooks() error {
	cwd, err := projectRoot()
	if err != nil {
		return err
	}

	hooksDir := filepath.Join(cwd, ".githooks")
	if err := verifyHooksDir(hooksDir); err != nil {
		return err
	}

	if err := gitConfig("core.hooksPath", ".githooks"); err != nil {
		return fmt.Errorf("set core.hooksPath: %w", err)
	}
	fmt.Println("Git hooks path configured: .githooks")

	if err := makeExecutable(filepath.Join(hooksDir, "pre-commit")); err != nil {
		fmt.Printf("  Note: %v\n", err)
	} else {
		fmt.Println("  .githooks/pre-commit: made executable")
	}
	return nil
}

func runUninstallHooks() error {
	if err := gitConfigUnset("core.hooksPath"); err != nil {
		return fmt.Errorf("unset core.hooksPath: %w", err)
	}
	fmt.Println("Git hooks path configuration removed")
	return nil
}

func projectRoot() (string, error) {
	root, err := paths.GitTopLevel(".")
	if err != nil {
		return os.Getwd()
	}
	return root, nil
}

func verifyHooksDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf(".githooks directory not found at %s\nCreate it first: mkdir -p .githooks", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists but is not a directory", dir)
	}
	return nil
}

func makeExecutable(path string) error {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return fmt.Errorf("%s not found, skipping chmod", filepath.Base(path))
	}
	if err != nil {
		return err
	}
	return os.Chmod(path, info.Mode()|0o111)
}

func gitConfig(key, value string) error {
	return exec.Command("git", "config", key, value).Run()
}

func gitConfigUnset(key string) error {
	cmd := exec.Command("git", "config", "--unset", key)
	// Exit code 5 means the key wasn't set — treat as success.
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 5 {
			fmt.Println("  (core.hooksPath was not set)")
			return nil
		}
		return err
	}
	return nil
}
