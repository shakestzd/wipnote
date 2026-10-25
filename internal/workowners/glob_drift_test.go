package workowners

import (
	"testing"

	"github.com/shakestzd/wipnote/internal/arch"
)

// TestGlobCopies_Drift pins where the two "workowners-compatible" glob
// matchers disagree over the union of pattern shapes either handles. They
// are not merged (feat-68f18d3b, plan-7bc7114c slice 8): arch.GlobMatch
// treats a bare "**" as match-all and normalises separators, matchPattern
// does neither, and choosing one is an owner decision. Two more glob
// matchers have their own semantics and are out of this comparison:
// migrate.matchGlob ("**" crosses any characters, no basename fallback)
// and guardprofile.matchGlob (segment-wise doublestar, no basename fallback).
func TestGlobCopies_Drift(t *testing.T) {
	cases := []struct {
		glob, path    string
		owners, archM bool
	}{
		{"*.go", "dir/foo.go", true, true},
		{"cmd/*.go", "cmd/main.go", true, true},
		{"cmd/*.go", "cmd/sub/main.go", false, false},
		{"internal/**", "internal/a/b.go", true, true},
		{"internal/**", "internal", true, true},
		{"**/*.md", "docs/a/README.md", true, true},
		{"cmd/**/*.go", "cmd/a/b/c.go", true, true},
		{"cmd/**/*.go", "lib/a.go", false, false},
		{"**", "any/path.go", false, true},         // ! bare ** is match-all only in arch
	}
	for _, c := range cases {
		if got := matchPattern(c.glob, c.path); got != c.owners {
			t.Errorf("workowners.matchPattern(%q, %q) = %v, want %v", c.glob, c.path, got, c.owners)
		}
		if got := arch.GlobMatch(c.glob, c.path); got != c.archM {
			t.Errorf("arch.GlobMatch(%q, %q) = %v, want %v", c.glob, c.path, got, c.archM)
		}
	}
}
