package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLaunchYoloDev_RefusesOnDirtyProtectedBranch: with isolation enforcement
// on and a dirty protected branch, launchYoloDev must refuse before the
// harness starts, as the default launcher does. A fake `claude` on PATH records
// any launch so the test fails if the harness is reached.
func TestLaunchYoloDev_RefusesOnDirtyProtectedBranch(t *testing.T) {
	repo := setupCarryRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WIPNOTE_ENFORCE_ISOLATION", "true")
	t.Setenv("WIPNOTE_PROJECT_DIR", repo)
	t.Setenv("CLAUDE_PROJECT_DIR", repo)

	mustWrite := func(rel, content string) {
		t.Helper()
		p := filepath.Join(repo, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(".wipnote/features/feat-aaaaaaaa.html", "<html></html>")
	mustWrite("plugin/.claude-plugin/plugin.json", "{}")
	// Dirty the protected branch (tracked file modified).
	mustWrite("README.md", "# dirty\n")

	bin := t.TempDir()
	marker := filepath.Join(bin, "launched")
	script := "#!/bin/sh\ntouch " + marker + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	// launchYoloDev requires a wipnote binary on PATH before it reaches the
	// launch plan; stub one so the test does not depend on the host install.
	if err := os.WriteFile(filepath.Join(bin, "wipnote"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	t.Chdir(repo)
	err := launchYoloDev("auto", "", "feat-aaaaaaaa", false, "", "", nil)
	if err == nil || !strings.Contains(err.Error(), "launch refused") {
		t.Fatalf("expected launch refused error, got %v", err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("harness was launched despite refusal")
	}
}
