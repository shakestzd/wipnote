package workitem

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKindForID_AllPrefixes pins the one prefix table: every prefix
// GenerateID mints (plus the legacy "pln-") maps to one kind and one
// collection, and anything else falls back ("work item" / "").
func TestKindForID_AllPrefixes(t *testing.T) {
	cases := []struct{ id, kind, dir string }{
		{"feat-1234abcd", "feature", "features"},
		{"bug-1234abcd", "bug", "bugs"},
		{"spk-1234abcd", "spike", "spikes"},
		{"trk-1234abcd", "track", "tracks"},
		{"plan-1234abcd", "plan", "plans"},
		{"pln-1234abcd", "plan", "plans"},
		{"spec-1234abcd", "spec", "specs"},
		{"feat-", "feature", "features"},
		{"spc-1234abcd", "work item", ""},
		{"sess-1234abcd", "work item", ""},
		{"feat1234", "work item", ""},
		{"FEAT-1234abcd", "work item", ""},
		{"", "work item", ""},
		{"127926be-6a1c-4045-a347-e42785ec5839", "work item", "sessions"},
	}
	for _, c := range cases {
		if got := KindForID(c.id); got != c.kind {
			t.Errorf("KindForID(%q) = %q, want %q", c.id, got, c.kind)
		}
		if got := collectionDirForID(c.id); got != c.dir {
			t.Errorf("collectionDirForID(%q) = %q, want %q", c.id, got, c.dir)
		}
	}
}

// TestKindForID_CoversGenerateID pins that every work-item kind GenerateID
// mints is in the prefix table, so a new kind is added in one place.
func TestKindForID_CoversGenerateID(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "spike", "track", "plan", "spec"} {
		id := GenerateID(kind, "title")
		if got := KindForID(id); got != kind {
			t.Errorf("KindForID(%q) = %q, want %q", id, got, kind)
		}
	}
}

// TestNodePath_ExactFlatFiles pins the shared exact-id lookup: it finds a
// flat <subdir>/<id>.html in any collection, prefers the first collection
// in order, and returns "" for a prefix, a missing id or a directory-backed
// track.
func TestNodePath_ExactFlatFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(rel string) string {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("<html></html>"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	feat := write("features/feat-1234abcd.html")
	spec := write("specs/spec-1234abcd.html")
	write("bugs/feat-1234abcd.html") // same id later in the order: features wins
	write("tracks/trk-1234abcd/index.html")

	cases := map[string]string{
		"feat-1234abcd": feat,
		"spec-1234abcd": spec,
		"feat-1234":     "",
		"bug-00000000":  "",
		"trk-1234abcd":  "",
	}
	for id, want := range cases {
		if got := NodePath(dir, id); got != want {
			t.Errorf("NodePath(%q) = %q, want %q", id, got, want)
		}
	}
}
