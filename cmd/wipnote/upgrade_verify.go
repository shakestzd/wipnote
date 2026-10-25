package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

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
