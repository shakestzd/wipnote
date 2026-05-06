package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// formulaPath returns the absolute path to Formula/wipnote.rb in the repo root.
func formulaPath(t *testing.T) string {
	t.Helper()
	root, err := projectRoot()
	if err != nil {
		t.Fatalf("could not determine project root: %v", err)
	}
	return filepath.Join(root, "Formula", "wipnote.rb")
}

func TestHomebrewFormulaExists(t *testing.T) {
	formula := formulaPath(t)
	if _, err := os.Stat(formula); os.IsNotExist(err) {
		t.Fatalf("Formula/wipnote.rb does not exist at %s", formula)
	}
}

func TestHomebrewFormulaStructure(t *testing.T) {
	formula := formulaPath(t)

	data, err := os.ReadFile(formula)
	if err != nil {
		t.Fatalf("could not read Formula/wipnote.rb: %v", err)
	}
	content := string(data)

	required := []struct {
		name    string
		pattern string
	}{
		{"class declaration", "class Wipnote < Formula"},
		{"desc field", "desc "},
		{"homepage field", "homepage "},
		{"version field", "version "},
		{"license field", "license "},
		{"on_macos block", "on_macos do"},
		{"on_linux block", "on_linux do"},
		{"darwin arm64 url", "darwin_arm64"},
		{"darwin amd64 url", "darwin_amd64"},
		{"linux arm64 url", "linux_arm64"},
		{"linux amd64 url", "linux_amd64"},
		{"sha256 field", "sha256"},
		{"install method", "def install"},
		{"bin.install", `bin.install "wipnote"`},
		{"test block", "test do"},
	}

	for _, req := range required {
		if !strings.Contains(content, req.pattern) {
			t.Errorf("Formula/wipnote.rb missing %s (expected to contain %q)", req.name, req.pattern)
		}
	}
}

func TestHomebrewFormulaURLPattern(t *testing.T) {
	formula := formulaPath(t)

	data, err := os.ReadFile(formula)
	if err != nil {
		t.Fatalf("could not read Formula/wipnote.rb: %v", err)
	}
	content := string(data)

	// Verify URLs reference the correct GitHub repo
	if !strings.Contains(content, "shakestzd/wipnote") {
		t.Error("Formula/wipnote.rb should reference shakestzd/wipnote repo")
	}

	// Verify URL uses releases/download pattern
	if !strings.Contains(content, "releases/download") {
		t.Error("Formula/wipnote.rb should use GitHub releases/download URL pattern")
	}

	// Verify archive format is tar.gz
	if !strings.Contains(content, ".tar.gz") {
		t.Error("Formula/wipnote.rb should reference .tar.gz archives")
	}
}
