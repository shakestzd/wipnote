package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	githubReleaseAPI = "https://api.github.com/repos/shakestzd/wipnote/releases/latest"
	downloadBaseURL  = "https://github.com/shakestzd/wipnote/releases/download"
)

func upgradeCmd() *cobra.Command {
	var checkOnly bool
	var pinVersion string

	cmd := &cobra.Command{
		Use:     "upgrade",
		Aliases: []string{"update"},
		Short:   "Upgrade wipnote to the latest (or specified) version",
		Long:    "Download and install the latest wipnote release from GitHub. Use --check to preview without installing.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpgrade(cmd.OutOrStdout(), checkOnly, pinVersion)
		},
	}
	cmd.Flags().BoolVar(&checkOnly, "check", false, "Show current vs latest version without installing")
	cmd.Flags().StringVar(&pinVersion, "version", "", "Install a specific version (e.g. 0.54.9)")
	return cmd
}

func runUpgrade(out io.Writer, checkOnly bool, pinVersion string) error {
	currentVer := strings.TrimPrefix(version, "v")

	// Determine target version.
	targetVer, err := resolveTargetVersion(pinVersion)
	if err != nil {
		return fmt.Errorf("resolving target version: %w", err)
	}

	if checkOnly {
		fmt.Fprintf(out, "current: %s\n", currentVer)
		fmt.Fprintf(out, "latest:  %s\n", targetVer)
		if targetVer == currentVer {
			fmt.Fprintln(out, "status:  up to date")
		} else {
			fmt.Fprintln(out, "status:  update available")
		}
		return nil
	}

	// Determine platform.
	goos, goarch := runtime.GOOS, runtime.GOARCH
	platformOS, platformArch, err := mapPlatform(goos, goarch)
	if err != nil {
		return err
	}

	archive := archiveName(targetVer, platformOS, platformArch)
	url := fmt.Sprintf("%s/v%s/%s", downloadBaseURL, targetVer, archive)

	fmt.Fprintf(out, "Downloading wipnote v%s for %s/%s...\n", targetVer, platformOS, platformArch)

	// Download to temp dir.
	tmpDir, err := os.MkdirTemp("", "wipnote-upgrade-*")
	if err != nil {
		return fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tarballPath := filepath.Join(tmpDir, archive)
	if err := downloadFile(url, tarballPath); err != nil {
		return fmt.Errorf("downloading release: %w", err)
	}

	// Verify SHA256 of the downloaded archive against the release's
	// checksums file. Fails the upgrade on mismatch — never install a
	// tampered or corrupted archive. If the checksums file cannot be
	// fetched (e.g. older release without one), the upgrade proceeds
	// with a warning so we never lock users out of older versions.
	//
	// TODO: add cosign keyless signature verification of the checksums
	// file once cosign Go SDK is acceptable as a dependency. For now,
	// `install.sh` performs the optional cosign step when available.
	checksumsURL := fmt.Sprintf("%s/v%s/wipnote_%s_checksums.txt", downloadBaseURL, targetVer, targetVer)
	if err := verifyArchiveChecksum(out, tarballPath, archive, checksumsURL); err != nil {
		return fmt.Errorf("verifying archive checksum: %w", err)
	}

	// Extract archive into an "extracted" subdir of the temp dir. This gives
	// us both the wipnote binary and the bundled plugin tree (since v0.59).
	extractRoot := filepath.Join(tmpDir, "extracted")
	if err := os.MkdirAll(extractRoot, 0o755); err != nil {
		return fmt.Errorf("creating extract dir: %w", err)
	}
	if err := extractArchive(tarballPath, extractRoot); err != nil {
		return fmt.Errorf("extracting archive: %w", err)
	}

	binaryName := "wipnote"
	extractedPath := filepath.Join(extractRoot, binaryName)
	if _, err := os.Stat(extractedPath); err != nil {
		return fmt.Errorf("binary %q not found in archive: %w", binaryName, err)
	}

	// Determine install destination.
	currentBin, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolving current binary path: %w", err)
	}
	currentBin, err = filepath.EvalSymlinks(currentBin)
	if err != nil {
		return fmt.Errorf("resolving symlinks for current binary: %w", err)
	}

	// Check if destination is writable.
	installDir := filepath.Dir(currentBin)
	if err := checkWritable(installDir); err != nil {
		return fmt.Errorf("install directory %s is not writable: %w\nTry: sudo wipnote upgrade, or reinstall to ~/.local/bin", installDir, err)
	}

	fmt.Fprintf(out, "Installing to %s...\n", currentBin)

	// Atomic replace: try os.Rename; fall back to copy on cross-device.
	if err := atomicReplace(extractedPath, currentBin); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	// Install every bundled tree the archive carries, to keep the binary and
	// its assets in lockstep. Older archives lack some (plugin/ and
	// codex-marketplace/ came in v0.59, antigravity-extension/ and mods/
	// later); a missing one is skipped silently.
	for _, tree := range bundledShareTrees {
		extracted := filepath.Join(extractRoot, tree)
		if info, err := os.Stat(extracted); err != nil || !info.IsDir() {
			continue
		}
		if err := installShareTree(extracted, tree); err != nil {
			fmt.Fprintf(out, "warning: failed to install bundled %s tree: %v\n", tree, err)
		} else {
			fmt.Fprintf(out, "Installed bundled %s tree to ~/.local/share/wipnote/%s\n", tree, tree)
		}
	}

	// Update ~/.local/share/wipnote/.binary-version so bootstrap fast-path works.
	updateVersionFile(targetVer)

	// Self-test: run the newly installed binary's version subcommand.
	fmt.Fprintln(out, "Verifying installed binary...")
	if err := verifySelfVersion(currentBin, targetVer); err != nil {
		fmt.Fprintf(out, "warning: version verification failed: %v\n", err)
		fmt.Fprintln(out, "The binary was installed but may not report the expected version.")
	} else {
		fmt.Fprintf(out, "wipnote v%s installed successfully.\n", targetVer)
	}
	return nil
}

// resolveTargetVersion returns the pinned version if provided, otherwise
// fetches the latest tag from the GitHub releases API.
func resolveTargetVersion(pinVersion string) (string, error) {
	if pinVersion != "" {
		return strings.TrimPrefix(pinVersion, "v"), nil
	}
	return fetchUpgradeVersion()
}

// fetchUpgradeVersion queries the GitHub releases API and returns the latest tag.
func fetchUpgradeVersion() (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(githubReleaseAPI)
	if err != nil {
		return "", fmt.Errorf("querying GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parsing GitHub API response: %w", err)
	}
	if payload.TagName == "" {
		return "", fmt.Errorf("GitHub API returned empty tag_name")
	}
	return strings.TrimPrefix(payload.TagName, "v"), nil
}

// mapPlatform converts GOOS/GOARCH to the archive naming used by goreleaser.
func mapPlatform(goos, goarch string) (string, string, error) {
	var os, arch string
	switch goos {
	case "darwin":
		os = "darwin"
	case "linux":
		os = "linux"
	default:
		return "", "", fmt.Errorf("unsupported OS: %s", goos)
	}
	switch goarch {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", goarch)
	}
	return os, arch, nil
}

// downloadFile fetches url and writes it to dest.
func downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}
