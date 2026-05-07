package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveInstallDir_Default(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".local", "bin")

	got, err := resolveInstallDir("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveInstallDir_Custom(t *testing.T) {
	tmp := t.TempDir()
	got, err := resolveInstallDir(tmp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != tmp {
		t.Errorf("got %q, want %q", got, tmp)
	}
}

func TestIsInPATH(t *testing.T) {
	tmp := t.TempDir()

	// Temporarily prepend tmp to PATH.
	orig := os.Getenv("PATH")
	t.Setenv("PATH", tmp+string(os.PathListSeparator)+orig)

	if !isInPATH(tmp) {
		t.Errorf("expected %q to be in PATH", tmp)
	}
	if isInPATH(filepath.Join(tmp, "nonexistent")) {
		t.Error("nonexistent subdir should not be in PATH")
	}
}

func TestCheckExistingTarget_Missing(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "wipnote")
	var buf bytes.Buffer

	done, err := checkExistingTarget(&buf, target, "/some/src", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if done {
		t.Error("expected done=false for missing target")
	}
}

func TestCheckExistingTarget_AlreadySymlinked(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "real-binary")
	target := filepath.Join(tmp, "wipnote")

	// Create a fake source file.
	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(src, target); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done, err := checkExistingTarget(&buf, target, src, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Error("expected done=true when already symlinked to same source")
	}
	if !strings.Contains(buf.String(), "already set up") {
		t.Errorf("expected 'already set up' in output, got: %s", buf.String())
	}
}

func TestCheckExistingTarget_DifferentFile_NoForce(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "wipnote")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	_, err := checkExistingTarget(&buf, target, "/new/src", false)
	if err == nil {
		t.Error("expected error when target exists and --force not set")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected --force hint in error, got: %v", err)
	}
}

func TestCheckExistingTarget_DifferentFile_Force(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "wipnote")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	done, err := checkExistingTarget(&buf, target, "/new/src", true)
	if err != nil {
		t.Fatalf("unexpected error with --force: %v", err)
	}
	if done {
		t.Error("expected done=false after forced removal")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected old target to be removed by --force")
	}
}

func TestCreateLink_Symlink(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "real-binary")
	target := filepath.Join(tmp, "link")

	if err := os.WriteFile(src, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := createLink(src, target); err != nil {
		t.Fatalf("createLink failed: %v", err)
	}

	// Verify it's a symlink pointing to src.
	dest, err := os.Readlink(target)
	if err != nil {
		t.Fatalf("expected symlink, got error: %v", err)
	}
	if dest != src {
		t.Errorf("symlink points to %q, want %q", dest, src)
	}
}

func TestPrintPATHInstructions(t *testing.T) {
	var buf bytes.Buffer
	printPATHInstructions(&buf, "/home/user/.local/bin")
	out := buf.String()

	if !strings.Contains(out, "/home/user/.local/bin") {
		t.Errorf("expected dir in output, got: %s", out)
	}
	if !strings.Contains(out, "export PATH") {
		t.Errorf("expected export PATH instruction, got: %s", out)
	}
	if !strings.Contains(out, "source ~/.zshrc") {
		t.Errorf("expected source instruction, got: %s", out)
	}
}

func TestRunSetupCLI_EndToEnd(t *testing.T) {
	tmp := t.TempDir()
	installDir := filepath.Join(tmp, "bin")

	// Use the current test binary as the "source binary" via a temp file.
	fakeSrc := filepath.Join(tmp, "fake-wipnote")
	// Write a shell script that returns a version line.
	script := "#!/bin/sh\necho 'wipnote dev (go)'\n"
	if err := os.WriteFile(fakeSrc, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	// We can't easily mock os.Executable, so test the install-dir + symlink path
	// by calling createLink directly and then verifying the output logic.
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(installDir, "wipnote")
	if err := createLink(fakeSrc, target); err != nil {
		t.Fatalf("createLink: %v", err)
	}

	// Confirm symlink exists.
	if _, err := os.Lstat(target); err != nil {
		t.Fatalf("target not created: %v", err)
	}

	// Exercise PATH instruction path (installDir not in PATH).
	t.Setenv("PATH", "/usr/bin:/bin")
	printPATHInstructions(&buf, installDir)
	if !strings.Contains(buf.String(), installDir) {
		t.Errorf("PATH instructions should mention install dir")
	}
}
