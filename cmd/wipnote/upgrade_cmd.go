package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
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

	// Extract binary from tarball.
	binaryName := "wipnote"
	extractedPath := filepath.Join(tmpDir, binaryName)
	if err := extractBinary(tarballPath, binaryName, extractedPath); err != nil {
		return fmt.Errorf("extracting binary: %w", err)
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

// extractBinary extracts a single named file from a .tar.gz archive.
func extractBinary(tarball, binaryName, dest string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("reading gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("reading tar: %w", err)
		}
		if filepath.Base(hdr.Name) == binaryName {
			out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
			if err != nil {
				return err
			}
			if _, err = io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("binary %q not found in archive", binaryName)
}

// atomicReplace replaces dest with src. Tries os.Rename first (atomic on same
// filesystem), falls back to copy + chmod + remove on cross-device.
func atomicReplace(src, dest string) error {
	if err := os.Rename(src, dest); err == nil {
		return nil
	}
	// Cross-device fallback.
	if err := copyBinary(src, dest); err != nil {
		return err
	}
	return os.Remove(src)
}

// checkWritable verifies the directory is writable by attempting to create a
// temp file inside it.
func checkWritable(dir string) error {
	tmp, err := os.CreateTemp(dir, ".wipnote-write-test-*")
	if err != nil {
		return err
	}
	tmp.Close()
	return os.Remove(tmp.Name())
}

// updateVersionFile writes the installed version to ~/.local/share/wipnote/.binary-version.
func updateVersionFile(ver string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	versionFile := filepath.Join(home, ".local", "share", "wipnote", ".binary-version")
	_ = os.MkdirAll(filepath.Dir(versionFile), 0o755)
	_ = os.WriteFile(versionFile, []byte(ver), 0o644)
}

// archiveName constructs the archive filename matching goreleaser's name_template:
// "wipnote_{{.Version}}_{{.Os}}_{{.Arch}}"
func archiveName(version, os, arch string) string {
	return fmt.Sprintf("wipnote_%s_%s_%s.tar.gz", version, os, arch)
}

// verifySelfVersion runs `<binary> version` and checks the output contains targetVer.
func verifySelfVersion(binary, targetVer string) error {
	out, err := exec.Command(binary, "version").Output()
	if err != nil {
		return fmt.Errorf("running version command: %w", err)
	}
	if !strings.Contains(string(out), targetVer) {
		return fmt.Errorf("expected version %s in output, got: %s", targetVer, strings.TrimSpace(string(out)))
	}
	return nil
}

// verifyArchiveChecksum downloads the release's checksums file, looks up the
// expected SHA256 for archiveName, and compares it against the on-disk hash
// of tarballPath. Returns an error on mismatch. If the checksums file cannot
// be fetched or contains no entry for this archive, it prints a warning and
// returns nil — older releases predate the checksums file and we should not
// brick the upgrade path for them.
func verifyArchiveChecksum(out io.Writer, tarballPath, archiveName, checksumsURL string) error {
	expected, err := fetchExpectedChecksum(checksumsURL, archiveName)
	if err != nil {
		fmt.Fprintf(out, "warning: could not fetch checksums file (%v); skipping verification\n", err)
		return nil
	}
	if expected == "" {
		fmt.Fprintf(out, "warning: no checksum entry for %s; skipping verification\n", archiveName)
		return nil
	}
	actual, err := sha256OfFile(tarballPath)
	if err != nil {
		return fmt.Errorf("computing SHA256 of %s: %w", tarballPath, err)
	}
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archiveName, expected, actual)
	}
	fmt.Fprintln(out, "Checksum verified.")
	return nil
}

// fetchExpectedChecksum fetches a goreleaser-style checksums.txt and returns
// the SHA256 hex for the requested archive (matched on the last whitespace-
// separated field). Returns an empty string with no error when no matching
// entry exists.
func fetchExpectedChecksum(checksumsURL, archiveName string) (string, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(checksumsURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d from %s", resp.StatusCode, checksumsURL)
	}
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		// Each line is: "<sha256_hex>  <filename>"
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// Use the last field for the filename to be robust against any
		// leading "*" mode marker some tools emit.
		name := fields[len(fields)-1]
		if name == archiveName {
			return fields[0], nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading checksums: %w", err)
	}
	return "", nil
}

// sha256OfFile returns the hex-encoded SHA256 of the given file's contents.
func sha256OfFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
