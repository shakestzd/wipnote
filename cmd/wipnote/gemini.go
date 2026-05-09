package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// geminiExtensionInstallDir returns the expected install directory for the
// wipnote Gemini extension.
func geminiExtensionInstallDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".gemini", "extensions", "wipnote")
}

// isGeminiExtensionInstalled reports whether the wipnote extension is already
// installed at the default location.
func isGeminiExtensionInstalled() bool {
	_, err := os.Stat(geminiExtensionInstallDir())
	return err == nil
}

// resolveGeminiExtensionRef returns the --ref value to use when installing the
// extension. When the binary version is known (non-"dev"), it returns
// "gemini-extension-v<version>". In dev mode it falls back to the latest
// matching tag on origin (using semver sort), and errors if that also fails.
func resolveGeminiExtensionRef(override string) (string, error) {
	if override != "" {
		return override, nil
	}
	if version != "dev" {
		return "gemini-extension-v" + version, nil
	}
	// Dev binary: ask git for the latest gemini-extension-v* tag on origin.
	// Use git's own semver sort (--sort=-version:refname) to ensure v0.10.0
	// sorts after v0.9.0 (lexicographic tail -1 would pick incorrectly).
	out, err := exec.Command("git", "ls-remote", "--sort=-version:refname", "--tags", "origin", "gemini-extension-v*").Output()
	if err != nil {
		return "", fmt.Errorf(
			"binary built in dev mode and git ls-remote failed: %w\n"+
				"Either build with a real version (wipnote build) or pass --ref <ref>", err)
	}
	// Each line: "<sha>\trefs/tags/<tag>"
	// git's --sort=-version:refname puts highest semver first, so we take the first.
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		tag := strings.TrimPrefix(parts[1], "refs/tags/")
		// Skip ^{} dereferenced tag entries.
		if strings.HasSuffix(tag, "^{}") {
			continue
		}
		// First non-deref tag is the highest semver.
		return tag, nil
	}
	return "", fmt.Errorf(
		"binary built in dev mode: no gemini-extension-v* tags found on origin\n" +
			"Pass --ref <ref> to specify the extension version explicitly")
}

// runGeminiInit installs the wipnote Gemini extension, idempotently.
// Corresponds to: wipnote gemini --init [--ref <ref>] [--force] [--dry-run]
func runGeminiInit(ref string, force, dryRun bool) error {
	installDir := geminiExtensionInstallDir()

	// Check idempotency BEFORE resolving ref. For dev builds (version == "dev"),
	// skipping ref resolution avoids a network call when already installed.
	if isGeminiExtensionInstalled() && !force {
		fmt.Printf("wipnote Gemini extension is already installed at %s\n", installDir)
		fmt.Println("To reinstall: wipnote gemini --init --force")
		fmt.Println("To launch:    wipnote gemini")
		return nil
	}

	resolvedRef, err := resolveGeminiExtensionRef(ref)
	if err != nil {
		return err
	}

	installArgs := []string{
		"extensions", "install",
		"shakestzd/wipnote",
		"--ref", resolvedRef,
		"--consent",
		"--skip-settings",
	}

	fmt.Printf("Installing wipnote Gemini extension...\n")
	fmt.Printf("  ref: %s\n", resolvedRef)

	if dryRun {
		fmt.Printf("[dry-run] gemini %s\n", strings.Join(installArgs, " "))
		return nil
	}

	geminiPath, err := exec.LookPath("gemini")
	if err != nil {
		return fmt.Errorf("gemini not found in PATH: %w\nInstall Gemini CLI first: https://github.com/google-gemini/gemini-cli", err)
	}

	out, runErr := exec.Command(geminiPath, installArgs...).CombinedOutput()
	if runErr != nil {
		return fmt.Errorf("gemini extensions install failed: %w\n%s", runErr, strings.TrimSpace(string(out)))
	}

	fmt.Println("wipnote Gemini extension installed.")
	fmt.Println()
	fmt.Println("Setup complete. Run: wipnote gemini")
	return nil
}

// interactiveGeminiExtensionInstall prompts the user to install the extension.
func interactiveGeminiExtensionInstall() {
	fmt.Println()
	fmt.Println("wipnote extension is not installed for Gemini CLI.")
	fmt.Println("The extension adds hooks, agents, skills, and slash commands.")
	fmt.Println()
	fmt.Print("Install wipnote Gemini extension? [y/N]: ")

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	choice := strings.TrimSpace(strings.ToLower(line))

	if choice == "y" || choice == "yes" {
		fmt.Println()
		if err := runGeminiInit("", false, false); err != nil {
			fmt.Fprintf(os.Stderr, "warning: extension installation failed: %v\n", err)
			fmt.Fprintf(os.Stderr, "  Run manually: wipnote gemini --init\n")
		} else {
			fmt.Println("Extension installed successfully.")
		}
	} else {
		fmt.Println("Continuing without extension. Run 'wipnote gemini --init' later to add it.")
	}
	fmt.Println()
}

// ensureGeminiExtensionOnLaunch is called by the default launcher.
var ensureGeminiExtensionOnLaunchFn = ensureGeminiExtensionOnLaunch
var isGeminiExtensionInstalledFn = isGeminiExtensionInstalled
var interactiveGeminiExtensionInstallFn = interactiveGeminiExtensionInstall

// ensureGeminiExtensionOnLaunch is called by the default launcher.
func ensureGeminiExtensionOnLaunch() {
	if !isGeminiExtensionInstalledFn() {
		interactiveGeminiExtensionInstallFn()
	}
}

func maybeEnsureGeminiExtensionOnLaunch(dryRun bool) {
	if dryRun {
		return
	}
	ensureGeminiExtensionOnLaunchFn()
}

// launchGeminiDefault launches Gemini interactively with wipnote env injection.
// Corresponds to: wipnote gemini
func launchGeminiDefault(trackID, featureID, worktreePath, workItem string, noWorktree bool, extraArgs []string, dryRun bool) error {
	projectRoot, _ := resolveProjectRoot()
	maybeEnsureGeminiExtensionOnLaunch(dryRun)

	// Work item attribution: emit `wipnote feature start <id>` before launching.
	if workItem != "" && !dryRun {
		if err := runFeatureStart(workItem); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start work item %s: %v\n", workItem, err)
		}
	}

	// Resolve worktree path.
	workDir := projectRoot
	wipnoteRoot := ""
	switch {
	case worktreePath != "":
		workDir = worktreePath
		wipnoteRoot = projectRoot
	case !noWorktree && trackID != "":
		wt, err := EnsureForTrack(trackID, projectRoot, os.Stdout)
		if err != nil {
			return err
		}
		workDir = wt
		wipnoteRoot = projectRoot
	case !noWorktree && featureID != "":
		wt, err := EnsureForFeature(featureID, projectRoot, os.Stdout)
		if err != nil {
			return err
		}
		workDir = wt
		wipnoteRoot = projectRoot
	}

	fmt.Println("Launching Gemini CLI with wipnote context...")
	return execGemini(geminiLaunchOpts{
		ExtraArgs:    extraArgs,
		ProjectRoot:  workDir,
		WorktreeRoot: workDir,
		WipnoteRoot:  wipnoteRoot,
		Mode:         geminiLaunchModeDefault,
		DryRun:       dryRun,
	})
}

// launchGeminiContinue resumes the latest Gemini session.
// Corresponds to: wipnote gemini --continue
func launchGeminiContinue(extraArgs []string, dryRun bool) error {
	projectRoot, _ := resolveProjectRoot()
	maybeEnsureGeminiExtensionOnLaunch(dryRun)
	fmt.Println("Resuming latest Gemini session...")
	return execGemini(geminiLaunchOpts{
		ResumeLast:  true,
		ExtraArgs:   extraArgs,
		ProjectRoot: projectRoot,
		Mode:        geminiLaunchModeContinue,
		DryRun:      dryRun,
	})
}

// launchGeminiResume resumes a specific Gemini session by index.
// Corresponds to: wipnote gemini --resume <N>
func launchGeminiResume(index string, extraArgs []string, dryRun bool) error {
	projectRoot, _ := resolveProjectRoot()
	maybeEnsureGeminiExtensionOnLaunch(dryRun)
	fmt.Printf("Resuming Gemini session %s...\n", index)
	return execGemini(geminiLaunchOpts{
		ResumeIndex: index,
		ExtraArgs:   extraArgs,
		ProjectRoot: projectRoot,
		Mode:        geminiLaunchModeContinue,
		DryRun:      dryRun,
	})
}

// geminiExtensionMetadata represents the install metadata for the wipnote extension.
type geminiExtensionMetadata struct {
	Source string `json:"source"`
	Type   string `json:"type"`
}

// isExtensionAlreadyLinkedToLocalPath checks if the wipnote extension is already
// linked (as a live pointer) to the specified local path. Returns true only if
// the metadata exists, matches the local path, and is a link type.
func isExtensionAlreadyLinkedToLocalPath(localExtPath string) bool {
	home, _ := os.UserHomeDir()
	metaPath := filepath.Join(home, ".gemini", "extensions", "wipnote", ".gemini-extension-install.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return false
	}

	var meta geminiExtensionMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}

	return meta.Type == "link" && meta.Source == localExtPath
}

// buildGeminiLinkArgs returns the args for `gemini extensions link`.
// Exported for testability.
func buildGeminiLinkArgs(localExtPath string) []string {
	return []string{"extensions", "link", localExtPath, "--consent"}
}

// launchGeminiDev links the local packages/gemini-extension and launches Gemini.
// Corresponds to: wipnote gemini --dev [--isolate]
func launchGeminiDev(isolate, dryRun bool, extraArgs []string) error {
	// Resolve the local extension path relative to the project root.
	localExtPath, err := resolveLocalGeminiExtension()
	if err != nil {
		return err
	}

	fmt.Printf("Launching Gemini CLI in dev mode...\n")
	fmt.Printf("  Local extension: %s\n", localExtPath)

	// Check idempotency: if already linked to this local path, skip the link exec.
	// If linked elsewhere or installed from a different source, uninstall first.
	if !dryRun {
		if isExtensionAlreadyLinkedToLocalPath(localExtPath) {
			fmt.Println("Extension already linked (live pointer to local source).")
		} else {
			// Check if there's a stale install that needs cleaning up.
			home, _ := os.UserHomeDir()
			metaPath := filepath.Join(home, ".gemini", "extensions", "wipnote", ".gemini-extension-install.json")
			if data, err := os.ReadFile(metaPath); err == nil {
				var meta geminiExtensionMetadata
				if json.Unmarshal(data, &meta) == nil && (meta.Type != "link" || meta.Source != localExtPath) {
					// Stale or wrong-source install — uninstall first.
					fmt.Println("Replacing existing wipnote extension install...")
					geminiPath, geminiErr := exec.LookPath("gemini")
					if geminiErr != nil {
						return fmt.Errorf("gemini not found in PATH: %w\nInstall Gemini CLI first: https://github.com/google-gemini/gemini-cli", geminiErr)
					}
					uninstallArgs := []string{"extensions", "uninstall", "wipnote"}
					// Surface uninstall errors rather than swallowing them.
					out, uninstallErr := exec.Command(geminiPath, uninstallArgs...).CombinedOutput()
					if uninstallErr != nil {
						return fmt.Errorf("gemini extensions uninstall (while replacing stale install) failed: %w\n%s", uninstallErr, strings.TrimSpace(string(out)))
					}
				}
			}

			// Link the extension (idempotent — it's a live pointer).
			linkArgs := buildGeminiLinkArgs(localExtPath)
			fmt.Println("Linking extension...")
			geminiPath, err := exec.LookPath("gemini")
			if err != nil {
				return fmt.Errorf("gemini not found in PATH: %w\nInstall Gemini CLI first: https://github.com/google-gemini/gemini-cli", err)
			}
			if out, linkErr := exec.Command(geminiPath, linkArgs...).CombinedOutput(); linkErr != nil {
				return fmt.Errorf("gemini extensions link failed: %w\n%s", linkErr, strings.TrimSpace(string(out)))
			}

			// Verify filesystem state: re-read the metadata file and validate its contents.
			// Check that type == "link" and source == localExtPath, not just that the file exists.
			// This catches cases where gemini may have used a different path or install method.
			home2, _ := os.UserHomeDir()
			metaPath2 := filepath.Join(home2, ".gemini", "extensions", "wipnote", ".gemini-extension-install.json")
			metaData, err := os.ReadFile(metaPath2)
			if err != nil {
				return fmt.Errorf("gemini extensions link appeared to succeed but %s was not created — the link may have been blocked by an interactive prompt in gemini. Check gemini extensions list and try: 'gemini extensions link %s --consent' manually", metaPath2, localExtPath)
			}

			var postLinkMeta geminiExtensionMetadata
			if err := json.Unmarshal(metaData, &postLinkMeta); err != nil {
				return fmt.Errorf("gemini extensions link produced invalid metadata at %s: %w", metaPath2, err)
			}

			if postLinkMeta.Type != "link" {
				return fmt.Errorf("gemini extensions link produced wrong type at %s: got %q, want \"link\"", metaPath2, postLinkMeta.Type)
			}

			if postLinkMeta.Source != localExtPath {
				return fmt.Errorf("gemini extensions link did not update source path: still points at %q, expected %q — try `gemini extensions uninstall wipnote` manually and retry", postLinkMeta.Source, localExtPath)
			}

			fmt.Println("Extension linked (live pointer to local source).")
		}
	}

	// Handle dry-run mode for link.
	if dryRun {
		linkArgs := buildGeminiLinkArgs(localExtPath)
		fmt.Printf("[dry-run] gemini %s\n", strings.Join(linkArgs, " "))
	}

	projectRoot, _ := resolveProjectRoot()

	ext := ""
	if isolate {
		ext = "wipnote"
	}

	return execGemini(geminiLaunchOpts{
		Extension:   ext,
		ExtraArgs:   extraArgs,
		ProjectRoot: projectRoot,
		Mode:        geminiLaunchModeDev,
		DryRun:      dryRun,
	})
}

// resolveLocalGeminiExtension returns the absolute path to packages/gemini-extension/
// by walking up from CWD to find the project root (directory containing .wipnote/).
func resolveLocalGeminiExtension() (string, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return "", fmt.Errorf("could not find project root (.wipnote/ directory not found)\n" +
			"Run from the wipnote project directory, or use wipnote gemini --init for the extension version")
	}
	projectRoot := filepath.Dir(wipnoteDir)
	extPath := filepath.Join(projectRoot, "packages", "gemini-extension")
	if _, statErr := os.Stat(extPath); os.IsNotExist(statErr) {
		return "", fmt.Errorf("packages/gemini-extension/ not found at %s\n"+
			"Run from the wipnote repo root, or use wipnote gemini --init for the published version",
			extPath)
	}
	abs, err := filepath.Abs(extPath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path for %s: %w", extPath, err)
	}
	return abs, nil
}

// geminiCmd returns the cobra command for `wipnote gemini`.
func geminiCmd() *cobra.Command {
	var init_, continue_, dev, force, isolate, listSessions, dryRun, noWorktree bool
	var resumeIndex, ref, trackID, featureID, worktreePath, workItem string

	cmd := &cobra.Command{
		Use:   "gemini",
		Short: "Launch Gemini CLI with wipnote context",
		Long: `Launch Gemini CLI with wipnote observability context.

Modes:
  wipnote gemini                      Launch Gemini interactively with wipnote env.
  wipnote gemini --init               Install the wipnote Gemini extension (idempotent).
  wipnote gemini --continue           Resume the latest Gemini session (gemini --resume latest).
  wipnote gemini --resume <N>         Resume a specific Gemini session by index.
  wipnote gemini --dev                Link packages/gemini-extension/ and launch Gemini.
  wipnote gemini --list-sessions      Pass-through: gemini --list-sessions.
  wipnote gemini --feature <id>       Launch in the feature's git worktree.
  wipnote gemini --track <id>         Launch in the track's git worktree.

Session indices come from: gemini --list-sessions.

Installation:
  wipnote gemini --init               Installs gemini-extension-v<version> from GitHub.
  wipnote gemini --init --ref <ref>   Override the extension ref (for pre-release testing).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch {
			case init_:
				return runGeminiInit(ref, force, dryRun)
			case listSessions:
				return execGemini(geminiLaunchOpts{ListSessions: true, DryRun: dryRun, Mode: geminiLaunchModeDefault})
			case dev:
				return launchGeminiDev(isolate, dryRun, args)
			case continue_:
				return launchGeminiContinue(args, dryRun)
			case resumeIndex != "":
				return launchGeminiResume(resumeIndex, args, dryRun)
			default:
				return launchGeminiDefault(trackID, featureID, worktreePath, workItem, noWorktree, args, dryRun)
			}
		},
	}

	cmd.Flags().BoolVar(&init_, "init", false, "Install the wipnote Gemini extension (idempotent)")
	cmd.Flags().BoolVar(&continue_, "continue", false, "Resume the latest Gemini session")
	cmd.Flags().BoolVar(&dev, "dev", false, "Link packages/gemini-extension/ as a live pointer and launch Gemini")
	cmd.Flags().BoolVar(&force, "force", false, "With --init: reinstall even if already installed")
	cmd.Flags().BoolVar(&isolate, "isolate", false, "With --dev: pass -e wipnote to suppress other extensions")
	cmd.Flags().BoolVar(&listSessions, "list-sessions", false, "Pass-through to gemini --list-sessions")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print what would happen without executing")
	cmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "Skip worktree creation (run in project root)")
	cmd.Flags().StringVar(&resumeIndex, "resume", "", "Resume a specific Gemini session by index (e.g. --resume 3)")
	cmd.Flags().StringVar(&ref, "ref", "", "With --init: override the extension ref (default: gemini-extension-v<version>)")
	cmd.Flags().StringVar(&trackID, "track", "", "Track ID to work on (e.g., trk-3719d8f3)")
	cmd.Flags().StringVar(&featureID, "feature", "", "Feature ID to work on (e.g., feat-15c458aa)")
	cmd.Flags().StringVar(&worktreePath, "worktree", "", "Explicit worktree path (overrides --track/--feature resolution)")
	cmd.Flags().StringVar(&workItem, "work-item", "", "Work item ID for attribution prefix (e.g., feat-15c458aa)")

	return cmd
}
