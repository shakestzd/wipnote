package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr runs fn with os.Stderr redirected to a pipe and returns the
// captured output.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w

	done := make(chan string)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	w.Close()
	os.Stderr = orig
	return <-done
}

// withCwd temporarily changes the process working directory for the duration
// of fn and restores it via t.Cleanup.
func withCwd(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestPrintProjectHeaderIfDifferent_SilentWhenInProject(t *testing.T) {
	projectRoot := setupTestProject(t)
	wipnoteDir := filepath.Join(projectRoot, ".wipnote")
	withCwd(t, projectRoot)

	out := captureStderr(t, func() {
		printProjectHeaderIfDifferent(wipnoteDir)
	})
	if out != "" {
		t.Errorf("expected silent output when CWD == project, got: %q", out)
	}
}

func TestPrintProjectHeaderIfDifferent_SilentWhenInSubdir(t *testing.T) {
	projectRoot := setupTestProject(t)
	sub := filepath.Join(projectRoot, "sub", "dir")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	wipnoteDir := filepath.Join(projectRoot, ".wipnote")
	withCwd(t, sub)

	out := captureStderr(t, func() {
		printProjectHeaderIfDifferent(wipnoteDir)
	})
	if out != "" {
		t.Errorf("expected silent output when CWD is inside project, got: %q", out)
	}
}

func TestPrintProjectHeaderIfDifferent_PrintsWhenOutsideProject(t *testing.T) {
	projectRoot := setupTestProject(t)
	otherDir := t.TempDir()
	wipnoteDir := filepath.Join(projectRoot, ".wipnote")
	withCwd(t, otherDir)

	out := captureStderr(t, func() {
		printProjectHeaderIfDifferent(wipnoteDir)
	})
	if !strings.Contains(out, "Project:") {
		t.Errorf("expected 'Project:' header, got: %q", out)
	}
	if !strings.Contains(out, "--project-dir to override") {
		t.Errorf("expected override hint, got: %q", out)
	}
}
