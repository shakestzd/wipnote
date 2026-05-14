package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractArchive_BinaryAndPlugin builds a synthetic tar.gz containing the
// post-Phase-A layout (wipnote binary at root, plugin/ subtree alongside) and
// verifies that extractArchive lays both out under destRoot with the right
// modes and content. Covers the contract that upgrade_cmd.go now relies on.
func TestExtractArchive_BinaryAndPlugin(t *testing.T) {
	// Build the archive in memory.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	entries := []struct {
		name     string
		mode     int64
		typeflag byte
		body     string
	}{
		{"wipnote", 0o755, tar.TypeReg, "fake-binary-bytes"},
		{"plugin/", 0o755, tar.TypeDir, ""},
		{"plugin/.claude-plugin/plugin.json", 0o644, tar.TypeReg, `{"name":"wipnote"}`},
		{"plugin/hooks/bin/bootstrap.sh", 0o755, tar.TypeReg, "#!/bin/sh\nexit 0\n"},
		{"plugin/agents/researcher.md", 0o644, tar.TypeReg, "# researcher\n"},
	}
	for _, e := range entries {
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     e.mode,
			Size:     int64(len(e.body)),
			Typeflag: e.typeflag,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write header %s: %v", e.name, err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("write body %s: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	dir := t.TempDir()
	tarballPath := filepath.Join(dir, "release.tar.gz")
	if err := os.WriteFile(tarballPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}

	destRoot := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatalf("mkdir destRoot: %v", err)
	}
	if err := extractArchive(tarballPath, destRoot); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}

	// Binary lifted to destRoot/wipnote with mode 0o755.
	binPath := filepath.Join(destRoot, "wipnote")
	binInfo, err := os.Stat(binPath)
	if err != nil {
		t.Fatalf("stat binary: %v", err)
	}
	if got := binInfo.Mode().Perm(); got != 0o755 {
		t.Errorf("binary mode = %o, want 0755", got)
	}
	body, _ := os.ReadFile(binPath)
	if string(body) != "fake-binary-bytes" {
		t.Errorf("binary body = %q", body)
	}

	// plugin.json is present.
	pluginJSON := filepath.Join(destRoot, "plugin", ".claude-plugin", "plugin.json")
	if _, err := os.Stat(pluginJSON); err != nil {
		t.Errorf("plugin.json missing: %v", err)
	}

	// bootstrap.sh keeps executable bit.
	bsPath := filepath.Join(destRoot, "plugin", "hooks", "bin", "bootstrap.sh")
	bsInfo, err := os.Stat(bsPath)
	if err != nil {
		t.Fatalf("stat bootstrap.sh: %v", err)
	}
	if bsInfo.Mode().Perm()&0o111 == 0 {
		t.Errorf("bootstrap.sh not executable: mode=%o", bsInfo.Mode().Perm())
	}

	// Agent markdown is plain.
	agentPath := filepath.Join(destRoot, "plugin", "agents", "researcher.md")
	if _, err := os.Stat(agentPath); err != nil {
		t.Errorf("researcher.md missing: %v", err)
	}
}

// TestExtractArchive_RejectsPathTraversal verifies that "../" entries cannot
// escape destRoot — a defensive check against malicious tarballs.
func TestExtractArchive_RejectsPathTraversal(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{
		Name:     "../escape.txt",
		Mode:     0o644,
		Size:     5,
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write header: %v", err)
	}
	if _, err := tw.Write([]byte("hello")); err != nil {
		t.Fatalf("write body: %v", err)
	}
	tw.Close()
	gz.Close()

	dir := t.TempDir()
	tarballPath := filepath.Join(dir, "bad.tar.gz")
	if err := os.WriteFile(tarballPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	destRoot := filepath.Join(dir, "extracted")
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		t.Fatalf("mkdir destRoot: %v", err)
	}

	if err := extractArchive(tarballPath, destRoot); err != nil {
		t.Fatalf("extractArchive: %v", err)
	}

	// Nothing should have been written.
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("path-traversal entry should not have been written outside destRoot")
	}
	if _, err := os.Stat(filepath.Join(destRoot, "..", "escape.txt")); err == nil {
		t.Fatal("path-traversal entry should not have been written above destRoot")
	}
}

// TestCopyDirRecursive_PreservesModes ensures that the cross-device fallback
// in installPluginTree preserves executable permissions (e.g. for hooks/bin/*.sh).
func TestCopyDirRecursive_PreservesModes(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := filepath.Join(t.TempDir(), "dst")

	exe := filepath.Join(srcRoot, "hooks", "bin", "run.sh")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}
	plain := filepath.Join(srcRoot, "plugin.json")
	if err := os.WriteFile(plain, []byte("{}"), 0o644); err != nil {
		t.Fatalf("write plain: %v", err)
	}

	if err := copyDirRecursive(srcRoot, dstRoot); err != nil {
		t.Fatalf("copyDirRecursive: %v", err)
	}

	exeOut, err := os.Stat(filepath.Join(dstRoot, "hooks", "bin", "run.sh"))
	if err != nil {
		t.Fatalf("stat exe out: %v", err)
	}
	if exeOut.Mode().Perm()&0o111 == 0 {
		t.Errorf("executable bit lost: mode=%o", exeOut.Mode().Perm())
	}

	plainOut, err := os.Stat(filepath.Join(dstRoot, "plugin.json"))
	if err != nil {
		t.Fatalf("stat plain out: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dstRoot, "plugin.json"))
	if !strings.Contains(string(body), "{}") {
		t.Errorf("plain copy body = %q", body)
	}
	if plainOut.Mode().Perm() != 0o644 {
		t.Errorf("plain mode = %o, want 0644", plainOut.Mode().Perm())
	}
}
