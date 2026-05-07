package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func setupCLICmd() *cobra.Command {
	var installDir string
	var force bool

	cmd := &cobra.Command{
		Use:   "setup-cli",
		Short: "Make wipnote available on your PATH",
		Long:  "Create a symlink so you can use wipnote from any terminal, not just within Claude Code sessions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSetupCLI(cmd.OutOrStdout(), installDir, force)
		},
	}
	cmd.Flags().StringVar(&installDir, "install-dir", "", "target directory (default: ~/.local/bin)")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing binary at target path")
	return cmd
}

func runSetupCLI(out io.Writer, installDir string, force bool) error {
	src, err := resolveSourceBinary()
	if err != nil {
		return fmt.Errorf("resolving source binary: %w", err)
	}

	targetDir, err := resolveInstallDir(installDir)
	if err != nil {
		return fmt.Errorf("resolving install directory: %w", err)
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("creating install directory %s: %w", targetDir, err)
	}

	target := filepath.Join(targetDir, "wipnote")

	done, err := checkExistingTarget(out, target, src, force)
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	if err := createLink(src, target); err != nil {
		return err
	}

	if err := verifyInstall(target); err != nil {
		fmt.Fprintf(out, "warning: verification failed: %v\n", err)
	} else {
		fmt.Fprintf(out, "wipnote installed at %s\n", target)
	}

	if !isInPATH(targetDir) {
		printPATHInstructions(out, targetDir)
	}
	return nil
}

// resolveSourceBinary returns the real path of the running binary.
func resolveSourceBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// resolveInstallDir expands the install directory, defaulting to ~/.local/bin.
func resolveInstallDir(installDir string) (string, error) {
	if installDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("determining home directory: %w", err)
		}
		return filepath.Join(home, ".local", "bin"), nil
	}
	return filepath.Abs(installDir)
}

// checkExistingTarget examines target. Returns (true, nil) when already set up,
// (false, err) when a blocking conflict exists, (false, nil) when clear to proceed.
func checkExistingTarget(out io.Writer, target, src string, force bool) (bool, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking %s: %w", target, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(target)
		if err == nil && existing == src {
			fmt.Fprintf(out, "already set up: %s -> %s\n", target, src)
			return true, nil
		}
	}

	if !force {
		return false, fmt.Errorf("%s already exists; use --force to overwrite", target)
	}
	return false, os.Remove(target)
}

// createLink tries to create a symlink; falls back to copying on cross-device error.
func createLink(src, target string) error {
	err := os.Symlink(src, target)
	if err == nil {
		return nil
	}
	// Cross-device link — fall back to a copy.
	return copyBinary(src, target)
}

// copyBinary copies src to target with executable permissions.
func copyBinary(src, target string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("opening source binary: %w", err)
	}
	defer in.Close()

	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("creating target binary: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("copying binary: %w", err)
	}
	return nil
}

// verifyInstall runs the installed binary with --version to confirm it works.
func verifyInstall(target string) error {
	_, err := exec.Command(target, "--version").Output()
	return err
}

// isInPATH reports whether dir is present in the PATH environment variable.
func isInPATH(dir string) bool {
	for _, p := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		if abs == dir {
			return true
		}
	}
	return false
}

// printPATHInstructions prints shell profile guidance when dir is not in PATH.
func printPATHInstructions(out io.Writer, dir string) {
	fmt.Fprintf(out, "\n%s is not in your PATH.\n", dir)
	fmt.Fprintf(out, "Add this to your shell profile (~/.zshrc or ~/.bashrc):\n")
	fmt.Fprintf(out, "  export PATH=%q\n", dir+":$PATH")
	fmt.Fprintf(out, "Then restart your shell or run: source ~/.zshrc\n")
}
