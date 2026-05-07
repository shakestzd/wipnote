package workowners

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_ValidFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKOWNERS")
	os.WriteFile(path, []byte("cmd/**  trk-abc\ninternal/*.go  feat-xyz\n"), 0o644)

	wf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if wf == nil {
		t.Fatal("expected non-nil file")
	}
	if len(wf.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(wf.Rules))
	}
	if wf.Rules[0].Pattern != "cmd/**" || wf.Rules[0].OwnerID != "trk-abc" {
		t.Errorf("rule 0: %+v", wf.Rules[0])
	}
}

func TestParse_MissingFile(t *testing.T) {
	wf, err := Parse("/nonexistent/WORKOWNERS")
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if wf != nil {
		t.Error("expected nil for missing file")
	}
}

func TestParse_CommentsAndBlanks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "WORKOWNERS")
	os.WriteFile(path, []byte("# comment\n\ncmd/**  trk-abc\n# another comment\n"), 0o644)

	wf, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(wf.Rules) != 1 {
		t.Fatalf("expected 1 rule (skipping comments/blanks), got %d", len(wf.Rules))
	}
}

func TestResolve_DoubleStarPrefix(t *testing.T) {
	wf := &File{Rules: []Rule{
		{Pattern: "cmd/wipnote/**", OwnerID: "trk-cli"},
	}}
	if got := wf.Resolve("cmd/wipnote/main.go"); got != "trk-cli" {
		t.Errorf("expected trk-cli, got %q", got)
	}
	if got := wf.Resolve("cmd/wipnote/sub/file.go"); got != "trk-cli" {
		t.Errorf("expected trk-cli for nested path, got %q", got)
	}
	if got := wf.Resolve("internal/db/schema.go"); got != "" {
		t.Errorf("expected empty for non-matching path, got %q", got)
	}
}

func TestResolve_GlobPattern(t *testing.T) {
	wf := &File{Rules: []Rule{
		{Pattern: "*.md", OwnerID: "trk-docs"},
	}}
	if got := wf.Resolve("README.md"); got != "trk-docs" {
		t.Errorf("expected trk-docs, got %q", got)
	}
	if got := wf.Resolve("docs/guide.md"); got != "trk-docs" {
		t.Errorf("expected trk-docs for nested .md, got %q", got)
	}
}

func TestResolve_LastMatchWins(t *testing.T) {
	wf := &File{Rules: []Rule{
		{Pattern: "cmd/**", OwnerID: "trk-general"},
		{Pattern: "cmd/wipnote/**", OwnerID: "trk-specific"},
	}}
	if got := wf.Resolve("cmd/wipnote/main.go"); got != "trk-specific" {
		t.Errorf("expected trk-specific (last match), got %q", got)
	}
	if got := wf.Resolve("cmd/other/main.go"); got != "trk-general" {
		t.Errorf("expected trk-general for cmd/other, got %q", got)
	}
}

func TestResolve_NilFile(t *testing.T) {
	var wf *File
	if got := wf.Resolve("anything.go"); got != "" {
		t.Errorf("expected empty for nil file, got %q", got)
	}
}

func TestMatchPattern_ExactPath(t *testing.T) {
	if !matchPattern("cmd/main.go", "cmd/main.go") {
		t.Error("exact match should succeed")
	}
	if matchPattern("cmd/main.go", "cmd/other.go") {
		t.Error("different file should not match")
	}
}

// Negative tests for ** false positives (roborev finding #2).

func TestResolve_DoubleStarSuffix_NoOvermatch(t *testing.T) {
	// "**/test.go" must NOT match "src/mytest.go" (partial filename match).
	wf := &File{Rules: []Rule{
		{Pattern: "**/test.go", OwnerID: "trk-test"},
	}}
	if got := wf.Resolve("src/mytest.go"); got != "" {
		t.Errorf("**/test.go should NOT match src/mytest.go, got %q", got)
	}
	// But it should match "src/test.go" (exact segment).
	if got := wf.Resolve("src/test.go"); got != "trk-test" {
		t.Errorf("**/test.go should match src/test.go, got %q", got)
	}
	// And "test.go" at root.
	if got := wf.Resolve("test.go"); got != "trk-test" {
		t.Errorf("**/test.go should match test.go, got %q", got)
	}
}

func TestResolve_MiddleDoubleStar_NoOvermatch(t *testing.T) {
	// "cmd/**/bar.go" must NOT match "cmd/sub/notbar.go".
	wf := &File{Rules: []Rule{
		{Pattern: "cmd/**/bar.go", OwnerID: "trk-cmd"},
	}}
	if got := wf.Resolve("cmd/sub/notbar.go"); got != "" {
		t.Errorf("cmd/**/bar.go should NOT match cmd/sub/notbar.go, got %q", got)
	}
	// But it should match "cmd/sub/bar.go".
	if got := wf.Resolve("cmd/sub/bar.go"); got != "trk-cmd" {
		t.Errorf("cmd/**/bar.go should match cmd/sub/bar.go, got %q", got)
	}
	// And "cmd/a/b/bar.go" (deep nesting).
	if got := wf.Resolve("cmd/a/b/bar.go"); got != "trk-cmd" {
		t.Errorf("cmd/**/bar.go should match cmd/a/b/bar.go, got %q", got)
	}
}

func TestResolve_DoubleStarGlob_NoOvermatch(t *testing.T) {
	// "**/.*go" should NOT be a valid concern — test "**/*.go" specifics.
	wf := &File{Rules: []Rule{
		{Pattern: "**/*.go", OwnerID: "trk-go"},
	}}
	// Should match any .go file at any depth.
	if got := wf.Resolve("cmd/main.go"); got != "trk-go" {
		t.Errorf("**/*.go should match cmd/main.go, got %q", got)
	}
	// Should NOT match .gob or .gohtml files.
	if got := wf.Resolve("data/file.gob"); got != "" {
		t.Errorf("**/*.go should NOT match file.gob, got %q", got)
	}
}

func TestResolve_BasenameGlob_NoSlash(t *testing.T) {
	// "*.md" without path should match at any depth (basename match).
	wf := &File{Rules: []Rule{
		{Pattern: "*.md", OwnerID: "trk-docs"},
	}}
	if got := wf.Resolve("README.md"); got != "trk-docs" {
		t.Errorf("*.md should match README.md, got %q", got)
	}
	if got := wf.Resolve("docs/guide.md"); got != "trk-docs" {
		t.Errorf("*.md should match docs/guide.md, got %q", got)
	}
	if got := wf.Resolve("docs/guide.txt"); got != "" {
		t.Errorf("*.md should NOT match guide.txt, got %q", got)
	}
}
