package main

import (
	"os"
	"strings"
	"testing"
)

// TestBuild_NoNotebookSync guards against re-adding the prototypes ->
// internal/notebook mirror step to `wipnote build`.
func TestBuild_NoNotebookSync(t *testing.T) {
	data, err := os.ReadFile("build.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	for _, banned := range []string{"syncNotebookFiles", "internal/notebook"} {
		if strings.Contains(src, banned) {
			t.Errorf("build.go still references %q", banned)
		}
	}
}
