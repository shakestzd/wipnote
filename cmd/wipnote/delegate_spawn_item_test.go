package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFeature(t *testing.T, wipnoteDir, id string) {
	t.Helper()
	dir := filepath.Join(wipnoteDir, "features")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A work-item prefix resolves to its canonical id before spawn stores and
// compares it, so one item cannot get two workers by spelling (PR 295 review).
func TestSpawnWorkItem_PrefixResolvesToCanonical(t *testing.T) {
	wd := filepath.Join(t.TempDir(), ".wipnote")
	writeFeature(t, wd, "feat-0a1b2c3d")
	for _, in := range []string{"feat-0a1b", "feat-0a1b2c3d"} {
		got, err := resolveSpawnWorkItem(wd, in)
		if err != nil || got != "feat-0a1b2c3d" {
			t.Fatalf("%s -> %q, %v", in, got, err)
		}
	}
}

// Other side: an unknown or ambiguous id is refused, and no id stays empty
// so spawn's own "no work item" refusal still applies.
func TestSpawnWorkItem_UnknownOrAmbiguousRefused(t *testing.T) {
	wd := filepath.Join(t.TempDir(), ".wipnote")
	writeFeature(t, wd, "feat-0a1b2c3d")
	writeFeature(t, wd, "feat-0a1b9999")
	if _, err := resolveSpawnWorkItem(wd, "feat-ffff"); err == nil {
		t.Fatal("unknown id accepted")
	}
	if _, err := resolveSpawnWorkItem(wd, "feat-0a1b"); err == nil {
		t.Fatal("ambiguous prefix accepted")
	}
	if got, err := resolveSpawnWorkItem(wd, ""); err != nil || got != "" {
		t.Fatalf("empty -> %q, %v", got, err)
	}
}
