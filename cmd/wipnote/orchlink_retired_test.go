package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRootFromCmd is the module root, two levels above cmd/wipnote.
func repoRootFromCmd(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// TestOrchlinkRetired pins slice 13's removal: internal/orchlink is gone,
// nothing in the repo's Go code imports it, and the lineage JSON schema note
// records that orch_sessions was dropped (orch history now arrives through
// `wipnote delegate import-orch` as delegations[]).
func TestOrchlinkRetired(t *testing.T) {
	root := repoRootFromCmd(t)
	if _, err := os.Stat(filepath.Join(root, "internal", "orchlink")); !os.IsNotExist(err) {
		t.Errorf("internal/orchlink must be deleted (stat err %v)", err)
	}
	// Built from parts so this file does not match itself.
	importPath := `"github.com/shakestzd/wipnote/internal/` + `orchlink"`
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".wipnote" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		if data, readErr := os.ReadFile(path); readErr == nil && strings.Contains(string(data), importPath) {
			t.Errorf("%s still imports internal/orchlink", path)
		}
		return nil
	})
	src, err := os.ReadFile("lineage.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), "orch_sessions and orch_error were dropped") {
		t.Error("the lineage JSON schema note must record that orch_sessions was dropped")
	}
}
