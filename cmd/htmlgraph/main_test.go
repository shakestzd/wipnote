package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/htmlgraph/internal/registry"
	"github.com/spf13/cobra"
)

// fakeCmd returns a minimal cobra.Command for use in PersistentPreRunE tests.
// It is wired up with a parent so PersistentPreRunE can walk the parent chain.
func fakeCmd(name string) *cobra.Command {
	return &cobra.Command{Use: name}
}

// setupTestProject creates a temporary directory with a .htmlgraph/
// subdirectory (mimicking a real HtmlGraph project) and returns the project
// root path and a cleanup function.
func setupTestProject(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	hgDir := filepath.Join(tmpDir, ".htmlgraph")
	if err := os.MkdirAll(hgDir, 0o755); err != nil {
		t.Fatalf("mkdir .htmlgraph: %v", err)
	}
	// Create a .git directory so looksLikeRealProject passes the git-ancestor
	// check introduced by the registry hardening (bug-cc41e3d2).
	if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}
	return tmpDir
}

// TestPersistentPreRunE_UpsertsRegistry verifies that PersistentPreRunE creates
// (or updates) an entry in the registry when invoked inside a project directory.
func TestPersistentPreRunE_UpsertsRegistry(t *testing.T) {
	projectDir := setupTestProject(t)
	homeDir := t.TempDir()

	// Override HOME so DefaultPath() writes to a test-isolated location.
	// Also clear XDG_DATA_HOME so DefaultPath falls back to the HOME-derived
	// path (TestMain sets XDG_DATA_HOME for suite-wide isolation, but these
	// tests control the registry path explicitly via HOME).
	t.Setenv("HOME", homeDir)
	t.Setenv("XDG_DATA_HOME", "")

	// Stub out git subprocess — return a known URL.
	origFn := getGitRemoteURLFn
	getGitRemoteURLFn = func(_ string) string { return "https://github.com/test/repo" }
	defer func() { getGitRemoteURLFn = origFn }()

	// Build a dummy rootCmd that exposes PersistentPreRunE.
	// NOTE: buildRootCmdForTest calls StringVar which RESETS projectDirFlag,
	// so we must set the flag AFTER building the rootCmd.
	rootCmd := buildRootCmdForTest(t)
	projectDirFlag = projectDir
	defer func() { projectDirFlag = "" }()
	cmd := fakeCmd("status")
	rootCmd.AddCommand(cmd)

	// Execute PersistentPreRunE directly (same as cobra would).
	if err := rootCmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("PersistentPreRunE: %v", err)
	}

	// Assert projects.json exists and contains our project.
	regPath := filepath.Join(homeDir, ".local", "share", "htmlgraph", "projects.json")
	data, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("projects.json not created: %v", err)
	}
	var entries []registry.Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		t.Fatalf("parse projects.json: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected at least one entry in projects.json")
	}
	found := false
	for _, e := range entries {
		if e.ProjectDir == projectDir {
			found = true
			if e.GitRemoteURL != "https://github.com/test/repo" {
				t.Errorf("GitRemoteURL: got %q, want %q", e.GitRemoteURL, "https://github.com/test/repo")
			}
			break
		}
	}
	if !found {
		t.Errorf("project %q not found in registry entries: %+v", projectDir, entries)
	}
}

// TestPersistentPreRunE_CachesGitRemote verifies that getGitRemoteURLFn is NOT
// called on the second invocation when the registry entry already has a
// GitRemoteURL populated.
func TestPersistentPreRunE_CachesGitRemote(t *testing.T) {
	projectDir := setupTestProject(t)
	homeDir := t.TempDir()

	t.Setenv("HOME", homeDir)
	// Clear XDG_DATA_HOME so DefaultPath falls back to the HOME-derived path
	// (TestMain sets XDG_DATA_HOME for suite-wide isolation).
	t.Setenv("XDG_DATA_HOME", "")

	// Pre-populate the registry with a GitRemoteURL so the second call should
	// skip the git subprocess entirely.
	regPath := filepath.Join(homeDir, ".local", "share", "htmlgraph", "projects.json")
	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := []registry.Entry{{
		ID:           "aabbccdd",
		ProjectDir:   projectDir,
		Name:         filepath.Base(projectDir),
		GitRemoteURL: "https://github.com/cached/repo",
		LastSeen:     "2026-01-01T00:00:00Z",
	}}
	data, _ := json.Marshal(existing)
	if err := os.WriteFile(regPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Count calls to the git subprocess stub.
	callCount := 0
	origFn := getGitRemoteURLFn
	getGitRemoteURLFn = func(_ string) string {
		callCount++
		return "https://github.com/should-not-be-called/repo"
	}
	defer func() { getGitRemoteURLFn = origFn }()

	rootCmd := buildRootCmdForTest(t)
	// Set flag AFTER buildRootCmdForTest (StringVar resets it).
	projectDirFlag = projectDir
	defer func() { projectDirFlag = "" }()
	cmd := fakeCmd("status")
	rootCmd.AddCommand(cmd)

	// First invocation — entry exists with URL, so git should NOT be called.
	if err := rootCmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("first PersistentPreRunE: %v", err)
	}
	// Second invocation — still cached.
	if err := rootCmd.PersistentPreRunE(cmd, nil); err != nil {
		t.Fatalf("second PersistentPreRunE: %v", err)
	}

	if callCount != 0 {
		t.Errorf("getGitRemoteURLFn called %d time(s); want 0 (should be cached)", callCount)
	}

	// Verify the cached URL was preserved in the registry.
	reloaded, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("read projects.json: %v", err)
	}
	var reloadedEntries []registry.Entry
	if err := json.Unmarshal(reloaded, &reloadedEntries); err != nil {
		t.Fatalf("parse projects.json: %v", err)
	}
	for _, e := range reloadedEntries {
		if e.ProjectDir == projectDir && e.GitRemoteURL != "https://github.com/cached/repo" {
			t.Errorf("GitRemoteURL changed: got %q, want %q", e.GitRemoteURL, "https://github.com/cached/repo")
		}
	}
}

// TestRootCommandDescription asserts that the root cobra command's Short and
// Long descriptions both contain "lineage" (case-insensitive), enforcing that
// the headline positioning introduced in feat-3418e582 stays in place.
func TestRootCommandDescription(t *testing.T) {
	root := buildRoot()
	if !strings.Contains(strings.ToLower(root.Short), "lineage") {
		t.Errorf("root.Short does not contain 'lineage': %q", root.Short)
	}
	if !strings.Contains(strings.ToLower(root.Long), "lineage") {
		t.Errorf("root.Long does not contain 'lineage': %q", root.Long)
	}
}

// buildRootCmdForTest returns a *cobra.Command that exposes the same
// PersistentPreRunE as the real rootCmd, but without AddCommand noise and
// without registering a real database (graceful degradation handles that).
//
// We expose the real PersistentPreRunE by building the real rootCmd inline
// using the same closure, which is easier than extracting it to a package-level
// function given that the binary is in package main.
//
// Alternatively we could just call the real main() and capture behavior, but
// calling PersistentPreRunE directly is more deterministic for unit tests.
func buildRootCmdForTest(t *testing.T) *cobra.Command {
	t.Helper()
	// Build a minimal rootCmd that mirrors main()'s setup but avoids AddCommand
	// noise. The PersistentPreRunE is attached in main() — we replicate just that
	// part here by using a test-local closure that delegates to the real
	// rootCmd.PersistentPreRunE. Since both live in package main, we can share
	// the same package-level variables (projectDirFlag, getGitRemoteURLFn).
	root := &cobra.Command{
		Use:           "htmlgraph",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.PersistentFlags().StringVar(&projectDirFlag, "project-dir", "", "")
	root.PersistentPreRunE = persistentPreRunE
	return root
}
