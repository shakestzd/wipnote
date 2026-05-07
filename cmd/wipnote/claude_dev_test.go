package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRequireWipnoteOnPathAcceptsWipnoteBinary(t *testing.T) {
	binDir := t.TempDir()
	wipnotePath := filepath.Join(binDir, "wipnote")
	if err := os.WriteFile(wipnotePath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake wipnote: %v", err)
	}
	t.Setenv("PATH", binDir)

	if err := requireWipnoteOnPath(); err != nil {
		t.Fatalf("requireWipnoteOnPath() error = %v, want nil", err)
	}
}

func TestRequireWipnoteOnPathRejectsOnlyLegacyBinary(t *testing.T) {
	binDir := t.TempDir()
	// Legacy binary name is intentionally insufficient after the wipnote rename.
	legacyPath := filepath.Join(binDir, "htmlgraph")
	if err := os.WriteFile(legacyPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake legacy binary: %v", err)
	}
	t.Setenv("PATH", binDir)

	err := requireWipnoteOnPath()
	if err == nil {
		t.Fatal("requireWipnoteOnPath() error = nil, want missing wipnote error")
	}
	if got := err.Error(); !strings.Contains(got, "wipnote binary not found on PATH") {
		t.Fatalf("error = %q, want wipnote PATH guidance", got)
	}
}
