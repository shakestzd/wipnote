package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// getBootstrapScriptPath returns absolute path to bootstrap.sh from the repo root.
func getBootstrapScriptPath(t *testing.T) string {
	// Get the directory where this test file is located
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("Could not determine current test file location")
	}

	// Walk up from cmd/wipnote/bootstrap_test.go to repo root
	repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(thisFile)))

	scriptPath := filepath.Join(repoRoot, "plugin", "hooks", "bin", "bootstrap.sh")
	return scriptPath
}

// TestBootstrapScriptExists verifies the bootstrap script is present.
func TestBootstrapScriptExists(t *testing.T) {
	scriptPath := getBootstrapScriptPath(t)

	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		t.Fatalf("bootstrap.sh not found at %s", scriptPath)
	}
}

// TestBootstrapScriptSyntax verifies the script is valid POSIX sh.
func TestBootstrapScriptSyntax(t *testing.T) {
	scriptPath := getBootstrapScriptPath(t)

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read bootstrap.sh: %v", err)
	}

	scriptContent := string(content)

	// Verify it starts with POSIX sh shebang
	if !strings.HasPrefix(scriptContent, "#!/bin/sh") {
		t.Error("bootstrap.sh must start with #!/bin/sh (POSIX sh only)")
	}

	// Verify no bash-specific constructs (note: [[:space:]] is POSIX, not bash)
	bashisms := []string{
		"#!/bin/bash",
		"${var:",
		"${!",
		"=~",
		"+(",
		"$'",
	}

	for _, bashism := range bashisms {
		if strings.Contains(scriptContent, bashism) {
			t.Errorf("bootstrap.sh contains bash-ism: %s", bashism)
		}
	}
}

// TestBootstrapScriptHasPathCheck verifies the script checks for PATH binary.
func TestBootstrapScriptHasPathCheck(t *testing.T) {
	scriptPath := getBootstrapScriptPath(t)

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read bootstrap.sh: %v", err)
	}

	scriptContent := string(content)

	// Verify the script contains "command -v wipnote" for PATH lookup
	if !strings.Contains(scriptContent, "command -v wipnote") {
		t.Error("bootstrap.sh must check PATH via 'command -v wipnote'")
	}

	// Verify it uses PATH_BINARY variable
	if !strings.Contains(scriptContent, "PATH_BINARY") {
		t.Error("bootstrap.sh must use PATH_BINARY variable for PATH check")
	}

	// Verify version comparison against EXPECTED_VERSION
	if !strings.Contains(scriptContent, "EXPECTED_VERSION") {
		t.Error("bootstrap.sh must compare against EXPECTED_VERSION")
	}
}

// TestBootstrapScriptHasRecursionGuard verifies the script doesn't exec itself.
func TestBootstrapScriptHasRecursionGuard(t *testing.T) {
	scriptPath := getBootstrapScriptPath(t)

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read bootstrap.sh: %v", err)
	}

	scriptContent := string(content)

	// Verify recursion guard logic — check for real path resolution
	guardPatterns := []string{
		"_real_path",
		"_self_path",
		"cd",
		"dirname",
		"!=",
	}

	for _, pattern := range guardPatterns {
		if !strings.Contains(scriptContent, pattern) {
			t.Errorf("bootstrap.sh recursion guard may be incomplete: missing %s", pattern)
		}
	}

	// Verify the guard prevents execution of bootstrap itself
	if !strings.Contains(scriptContent, "if [ \"${_real_path}\" != \"${_self_path}\" ]") {
		t.Error("bootstrap.sh must have explicit guard comparing _real_path != _self_path")
	}
}

// TestBootstrapScriptStructure verifies logical flow: PATH check → fast path → slow path.
func TestBootstrapScriptStructure(t *testing.T) {
	scriptPath := getBootstrapScriptPath(t)

	content, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("Failed to read bootstrap.sh: %v", err)
	}

	scriptContent := string(content)

	// Find line numbers of key sections
	pathCheckIdx := strings.Index(scriptContent, "command -v wipnote")
	fastPathIdx := strings.Index(scriptContent, "Fast path:")
	slowPathIdx := strings.Index(scriptContent, "Slow path:")

	if pathCheckIdx == -1 {
		t.Error("PATH check section not found")
	}
	if fastPathIdx == -1 {
		t.Error("Fast path comment not found")
	}
	if slowPathIdx == -1 {
		t.Error("Slow path comment not found")
	}

	// Verify order: PATH check → fast path → slow path
	if pathCheckIdx > 0 && fastPathIdx > 0 {
		if pathCheckIdx > fastPathIdx {
			t.Error("PATH check must come BEFORE fast path check")
		}
	}
	if fastPathIdx > 0 && slowPathIdx > 0 {
		if fastPathIdx > slowPathIdx {
			t.Error("Fast path check must come BEFORE slow path (download)")
		}
	}
}

// BenchmarkBootstrapLogic measures script parsing time (not execution).
func BenchmarkBootstrapLogic(b *testing.B) {
	t := &testing.T{}
	scriptPath := getBootstrapScriptPath(t)

	scriptContent, err := os.ReadFile(scriptPath)
	if err != nil {
		b.Fatalf("Failed to read bootstrap.sh: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bytes.Count(scriptContent, []byte("command -v"))
	}
}
