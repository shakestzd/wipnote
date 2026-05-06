package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestInstallScriptExists validates that install.sh exists at project root.
func TestInstallScriptExists(t *testing.T) {
	// Relative to this test file, go back to repo root
	// cmd/htmlgraph/install_script_test.go -> repo root is 2 levels up
	scriptPath := filepath.Join("..", "..", "install.sh")

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("install.sh not found at %s: %v", scriptPath, err)
	}

	if info.IsDir() {
		t.Fatalf("install.sh is a directory, not a file")
	}
}

// TestInstallScriptPOSIXShell validates install.sh is valid POSIX sh.
func TestInstallScriptPOSIXShell(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "install.sh")

	// Run `sh -n install.sh` to check syntax without executing
	cmd := exec.Command("sh", "-n", scriptPath)
	if err := cmd.Run(); err != nil {
		t.Fatalf("install.sh failed POSIX sh syntax check: %v", err)
	}
}

// TestInstallScriptTagFormat validates install.sh uses v${VERSION} tag format.
func TestInstallScriptTagFormat(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "install.sh")

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read install.sh: %v", err)
	}

	contentStr := string(content)

	// Should use v${VERSION} format, not go/v${VERSION}
	if strings.Contains(contentStr, "go/v") {
		t.Errorf("install.sh contains 'go/v' tag prefix, should use 'v' prefix only")
	}

	// Check that it references GitHub releases with v prefix
	if !strings.Contains(contentStr, "releases/download/v") {
		t.Errorf("install.sh should use 'releases/download/v' for GitHub releases")
	}
}

// TestInstallScriptArchiveFormat validates archive naming matches goreleaser output.
func TestInstallScriptArchiveFormat(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "install.sh")

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read install.sh: %v", err)
	}

	contentStr := string(content)

	// Should use underscore-separated format: wipnote_${VERSION}_${OS}_${ARCH}.tar.gz
	// NOT dash-separated: wipnote-${OS}-${ARCH}.tar.gz
	if !strings.Contains(contentStr, "wipnote_") {
		t.Errorf("install.sh should use underscore-separated archive name (wipnote_VERSION_OS_ARCH.tar.gz)")
	}

	// Make sure it doesn't have the old dash format from bootstrap.sh
	if strings.Contains(contentStr, "wipnote-${") || strings.Contains(contentStr, "wipnote-${PLATFORM") {
		t.Errorf("install.sh should not use dash-separated archive name (wipnote-OS-ARCH.tar.gz)")
	}
}

// TestInstallScriptHelpFlag validates --help flag is present.
func TestInstallScriptHelpFlag(t *testing.T) {
	scriptPath := filepath.Join("..", "..", "install.sh")

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read install.sh: %v", err)
	}

	contentStr := string(content)

	// Should handle --help flag
	if !strings.Contains(contentStr, "--help") {
		t.Errorf("install.sh should support --help flag")
	}
}
