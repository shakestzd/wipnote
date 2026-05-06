package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- validateDescriptionForHostPaths tests -----------------------------------

// TestValidateDescriptionForHostPaths_Clean passes on clean descriptions.
func TestValidateDescriptionForHostPaths_Clean(t *testing.T) {
	cases := []string{
		"",
		"implement the feature",
		"see ./cmd/main.go for details",
		"relative/path/to/file.go",
		"screenshot.png shows the result",
	}
	for _, desc := range cases {
		if err := validateDescriptionForHostPaths(desc, false); err != nil {
			t.Errorf("expected no error for %q, got: %v", desc, err)
		}
	}
}

// TestValidateDescriptionForHostPaths_RejectsWorkspaces rejects /workspaces/ paths.
func TestValidateDescriptionForHostPaths_RejectsWorkspaces(t *testing.T) {
	desc := "see /workspaces/htmlgraph/Screenshot_2025.png for context"
	err := validateDescriptionForHostPaths(desc, false)
	if err == nil {
		t.Fatal("expected error for /workspaces/ path, got nil")
	}
	if !strings.Contains(err.Error(), "/workspaces/htmlgraph/") {
		t.Errorf("error should mention the offending path; got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-host-paths") {
		t.Errorf("error should mention --allow-host-paths bypass; got: %v", err)
	}
}

// TestValidateDescriptionForHostPaths_RejectsHomePath rejects /home/ paths.
func TestValidateDescriptionForHostPaths_RejectsHomePath(t *testing.T) {
	desc := "config lives at /home/vscode/.config/tool.yaml"
	err := validateDescriptionForHostPaths(desc, false)
	if err == nil {
		t.Fatal("expected error for /home/ path, got nil")
	}
	if !strings.Contains(err.Error(), "/home/vscode/") {
		t.Errorf("error should mention the offending path; got: %v", err)
	}
}

// TestValidateDescriptionForHostPaths_RejectsUsersPaths rejects /Users/ paths.
func TestValidateDescriptionForHostPaths_RejectsUsersPaths(t *testing.T) {
	desc := "file at /Users/alice/projects/htmlgraph/main.go"
	err := validateDescriptionForHostPaths(desc, false)
	if err == nil {
		t.Fatal("expected error for /Users/ path, got nil")
	}
	if !strings.Contains(err.Error(), "/Users/alice/") {
		t.Errorf("error should mention the offending path; got: %v", err)
	}
}

// TestValidateDescriptionForHostPaths_AllowHostPathsBypass passes with --allow-host-paths.
func TestValidateDescriptionForHostPaths_AllowHostPathsBypass(t *testing.T) {
	desc := "/workspaces/htmlgraph/foo.png embedded here"
	if err := validateDescriptionForHostPaths(desc, true); err != nil {
		t.Errorf("expected no error when allowHostPaths=true, got: %v", err)
	}
}

// TestValidateDescriptionForHostPaths_CIRunnerAllowed passes for /home/runner/ (CI).
func TestValidateDescriptionForHostPaths_CIRunnerAllowed(t *testing.T) {
	desc := "artifact at /home/runner/work/htmlgraph/htmlgraph/out.tar.gz"
	if err := validateDescriptionForHostPaths(desc, false); err != nil {
		t.Errorf("expected /home/runner/ to be allowed, got: %v", err)
	}
}

// TestScanFileForHostPaths_BadSample verifies that the bad-path fixture is flagged.
func TestScanFileForHostPaths_BadSample(t *testing.T) {
	fixture := filepath.Join("testdata", "bad-path-sample.html")
	violations, err := scanFileForHostPaths(fixture, fixture)
	if err != nil {
		t.Fatalf("scanFileForHostPaths error: %v", err)
	}
	if len(violations) == 0 {
		t.Error("expected violations for bad-path-sample.html, got none")
	}

	// Verify at least the /Users/fakeuser/ path is flagged.
	found := false
	for _, v := range violations {
		if v.matched == "/Users/fakeuser/" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /Users/fakeuser/ to be flagged; got violations: %v", violations)
	}
}

// TestScanFileForHostPaths_CleanSample verifies that the clean fixture passes.
func TestScanFileForHostPaths_CleanSample(t *testing.T) {
	fixture := filepath.Join("testdata", "clean-sample.html")
	violations, err := scanFileForHostPaths(fixture, fixture)
	if err != nil {
		t.Fatalf("scanFileForHostPaths error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected no violations for clean-sample.html, got: %v", violations)
	}
}

// TestScanFileForHostPaths_CIRunnerAllowed verifies /home/runner/ is not flagged.
func TestScanFileForHostPaths_CIRunnerAllowed(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "ci.html")
	if err := os.WriteFile(f, []byte("<p>/home/runner/work/htmlgraph/htmlgraph</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	violations, err := scanFileForHostPaths(f, "ci.html")
	if err != nil {
		t.Fatalf("scanFileForHostPaths error: %v", err)
	}
	if len(violations) != 0 {
		t.Errorf("expected /home/runner/ to be allowed; got violations: %v", violations)
	}
}

// TestScanFileForHostPaths_AllPatterns checks each host-local pattern is detected.
func TestScanFileForHostPaths_AllPatterns(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantHit bool
	}{
		{"macOS Users", "path=/Users/alice/project", true},
		{"Linux home", "path=/home/bob/project", true},
		{"Codespaces", "path=/workspaces/charlie/repo", true},
		{"macOS tmp", "path=/private/var/folders/abc/xyz", true},
		{"CI runner allowed", "path=/home/runner/work/repo", false},
		{"generic usr", "path=/usr/local/bin/tool", false},
		{"relative path", "path=./cmd/main.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			f := filepath.Join(tmp, "sample.txt")
			if err := os.WriteFile(f, []byte(tc.content), 0o644); err != nil {
				t.Fatal(err)
			}
			violations, err := scanFileForHostPaths(f, "sample.txt")
			if err != nil {
				t.Fatalf("scanFileForHostPaths error: %v", err)
			}
			if tc.wantHit && len(violations) == 0 {
				t.Errorf("expected violation for %q, got none", tc.content)
			}
			if !tc.wantHit && len(violations) != 0 {
				t.Errorf("expected no violation for %q, got: %v", tc.content, violations)
			}
		})
	}
}

// TestLoadHostPathAllowlist verifies the allowlist loader skips comments and blanks.
func TestLoadHostPathAllowlist(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "allowlist.txt")
	content := "# comment\n\n.wipnote/bugs/bug-4b6d8369.html\n.claude/settings.local.json\n"
	if err := os.WriteFile(f, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	allowlist, err := loadHostPathAllowlist(f)
	if err != nil {
		t.Fatalf("loadHostPathAllowlist error: %v", err)
	}
	if len(allowlist) != 2 {
		t.Errorf("expected 2 entries, got %d: %v", len(allowlist), allowlist)
	}
	if !allowlist[".wipnote/bugs/bug-4b6d8369.html"] {
		t.Error("expected .wipnote/bugs/bug-4b6d8369.html in allowlist")
	}
	if !allowlist[".claude/settings.local.json"] {
		t.Error("expected .claude/settings.local.json in allowlist")
	}
}

// TestLoadHostPathAllowlist_Missing verifies that a missing allowlist is not fatal.
func TestLoadHostPathAllowlist_Missing(t *testing.T) {
	_, err := loadHostPathAllowlist("/does/not/exist/allowlist.txt")
	if err == nil {
		t.Error("expected error for missing allowlist file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected os.IsNotExist error, got: %v", err)
	}
}

// TestScanHostPathFiles_AllowlistSkipsFile verifies allowlisted files are not scanned.
func TestScanHostPathFiles_AllowlistSkipsFile(t *testing.T) {
	tmp := t.TempDir()

	// Write a file with violations.
	bad := filepath.Join(tmp, "bad.html")
	if err := os.WriteFile(bad, []byte("<p>/Users/fakeuser/project</p>"), 0o644); err != nil {
		t.Fatal(err)
	}

	allowlist := map[string]bool{"bad.html": true}
	violations, scanned, err := scanHostPathFiles(tmp, []string{bad}, allowlist)
	if err != nil {
		t.Fatalf("scanHostPathFiles error: %v", err)
	}
	if scanned != 0 {
		t.Errorf("expected 0 files scanned (allowlisted), got %d", scanned)
	}
	if len(violations) != 0 {
		t.Errorf("expected 0 violations for allowlisted file, got %d", len(violations))
	}
}

// TestFullScopeFiles_SkipsBinaryAndLocal verifies that the scope collector excludes
// htmlgraph.db and settings.local.json.
func TestFullScopeFiles_SkipsBinaryAndLocal(t *testing.T) {
	tmp := t.TempDir()

	hgDir := filepath.Join(tmp, ".wipnote")
	claudeDir := filepath.Join(tmp, ".claude")
	for _, d := range []string{hgDir, claudeDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// Create files that should be excluded.
	for _, name := range []string{
		filepath.Join(hgDir, "htmlgraph.db"),
		filepath.Join(claudeDir, "settings.local.json"),
	} {
		if err := os.WriteFile(name, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Create files that should be included.
	included := filepath.Join(hgDir, "bugs", "bug-abc.html")
	if err := os.MkdirAll(filepath.Dir(included), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(included, []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, err := fullScopeFiles(tmp)
	if err != nil {
		t.Fatalf("fullScopeFiles error: %v", err)
	}

	for _, f := range files {
		base := filepath.Base(f)
		if base == "htmlgraph.db" || base == "settings.local.json" {
			t.Errorf("fullScopeFiles returned excluded file: %s", f)
		}
	}

	found := false
	for _, f := range files {
		if f == included {
			found = true
		}
	}
	if !found {
		t.Error("fullScopeFiles did not include bug HTML file")
	}
}
