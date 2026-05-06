package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWipResetWithoutForceError(t *testing.T) {
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	projectDirFlag = tmpDir
	defer func() { projectDirFlag = "" }()

	trackID := testSetupTrack(t, hgDir)

	if err := testCreate("feature", "In-progress Feature", trackID, "high", true, false); err != nil {
		t.Fatalf("create feature: %v", err)
	}

	// Try to reset without --force
	err := runWipReset(false)
	if err == nil {
		t.Fatal("expected error when calling runWipReset without --force, got nil")
	}

	// Check that error message contains count and --force hint
	errMsg := err.Error()
	if !stringContainsSubstring(errMsg, "--force") {
		t.Errorf("error message should mention --force: %q", errMsg)
	}
	if !stringContainsSubstring(errMsg, "1") {
		t.Errorf("error message should contain item count (1): %q", errMsg)
	}
}

func TestWipResetWithoutForceErrorMultipleItems(t *testing.T) {
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	projectDirFlag = tmpDir
	defer func() { projectDirFlag = "" }()

	trackID := testSetupTrack(t, hgDir)

	if err := testCreate("feature", "Feature 1", trackID, "high", true, false); err != nil {
		t.Fatalf("create feature 1: %v", err)
	}
	if err := testCreate("feature", "Feature 2", trackID, "high", true, false); err != nil {
		t.Fatalf("create feature 2: %v", err)
	}
	if err := testCreate("bug", "Bug 1", trackID, "high", true, false); err != nil {
		t.Fatalf("create bug 1: %v", err)
	}

	// Try to reset without --force
	err := runWipReset(false)
	if err == nil {
		t.Fatal("expected error when calling runWipReset without --force, got nil")
	}

	// Check that error message contains count (3) and --force hint
	errMsg := err.Error()
	if !stringContainsSubstring(errMsg, "--force") {
		t.Errorf("error message should mention --force: %q", errMsg)
	}
	if !stringContainsSubstring(errMsg, "3") {
		t.Errorf("error message should contain item count (3): %q", errMsg)
	}
}

func TestWipResetWithForceSucceeds(t *testing.T) {
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks", "plans", "specs"} {
		if err := os.MkdirAll(filepath.Join(hgDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	projectDirFlag = tmpDir
	defer func() { projectDirFlag = "" }()

	trackID := testSetupTrack(t, hgDir)

	if err := testCreate("feature", "In-progress Feature", trackID, "high", true, false); err != nil {
		t.Fatalf("create feature: %v", err)
	}

	// Reset with --force should succeed
	err := runWipReset(true)
	if err != nil {
		t.Fatalf("expected success with --force, got error: %v", err)
	}
}

// stringContainsSubstring is a helper to check if a string contains a substring
func stringContainsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
