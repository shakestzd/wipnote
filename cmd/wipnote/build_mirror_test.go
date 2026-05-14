package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMirrorPluginTree_PreservesContentAndModes ensures `wipnote build` will
// lay down a faithful copy of the source plugin/ tree under ~/.local/share,
// including executable bits on hooks/bin/*.sh.
func TestMirrorPluginTree_PreservesContentAndModes(t *testing.T) {
	srcRoot := t.TempDir()
	dstRoot := filepath.Join(t.TempDir(), "dst")

	// Build a tiny source plugin tree mirroring the real layout.
	files := map[string]struct {
		body string
		mode os.FileMode
	}{
		".claude-plugin/plugin.json": {`{"name":"wipnote","version":"0.0.0"}`, 0o644},
		"hooks/bin/bootstrap.sh":     {"#!/bin/sh\nexit 0\n", 0o755},
		"agents/researcher.md":       {"# researcher\n", 0o644},
	}
	for rel, f := range files {
		full := filepath.Join(srcRoot, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(f.body), f.mode); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}

	// Pre-existing dst — mirrorPluginTree must replace it.
	stale := filepath.Join(dstRoot, "stale.txt")
	if err := os.MkdirAll(dstRoot, 0o755); err != nil {
		t.Fatalf("mkdir dst: %v", err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0o644); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := mirrorPluginTree(srcRoot, dstRoot); err != nil {
		t.Fatalf("mirrorPluginTree: %v", err)
	}

	// Stale file is gone.
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("stale file %s was not removed", stale)
	}

	// Each source file is present in dst with correct mode/content.
	for rel, f := range files {
		out := filepath.Join(dstRoot, rel)
		info, err := os.Stat(out)
		if err != nil {
			t.Errorf("missing %s in dst: %v", rel, err)
			continue
		}
		if rel == "hooks/bin/bootstrap.sh" {
			if info.Mode().Perm()&0o111 == 0 {
				t.Errorf("bootstrap.sh lost executable bit: mode=%o", info.Mode().Perm())
			}
		} else {
			if info.Mode().Perm()&0o111 != 0 {
				t.Errorf("%s unexpectedly executable: mode=%o", rel, info.Mode().Perm())
			}
		}
		body, _ := os.ReadFile(out)
		if string(body) != f.body {
			t.Errorf("%s body mismatch: got %q want %q", rel, body, f.body)
		}
	}
}

// TestMirrorPluginTree_MissingSourceFails ensures we surface a clear error
// when the source plugin/ directory is missing (vs silently leaving dst stale).
func TestMirrorPluginTree_MissingSourceFails(t *testing.T) {
	dstRoot := filepath.Join(t.TempDir(), "dst")
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := mirrorPluginTree(missing, dstRoot); err == nil {
		t.Fatal("expected error when src missing, got nil")
	}
}
