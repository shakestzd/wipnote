package main

import (
	"bytes"
	"strings"
	"testing"
)

const dupSourceBlock = `func load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	cfg.applyDefaults()
	return &cfg, nil
}
`

// TestDupEvidence_ReportsCopiedBlock: a block added in this change that also
// appears elsewhere is reported as evidence for the reviewer, with both
// locations, so they can judge whether the copies must change together.
func TestDupEvidence_ReportsCopiedBlock(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{"a.go": "package p\n\n" + dupSourceBlock})
	writeRepoFile(t, dir, "b.go", "package p\n\n"+strings.Replace(dupSourceBlock, "func load(", "func loadAgain(", 1))

	pairs := scanDupEvidence(dir, resolveSizeBase(dir, "main"))
	if len(pairs) != 1 {
		t.Fatalf("pairs = %+v, want one b.go/a.go pair", pairs)
	}
	p := pairs[0]
	if p.Path != "b.go" || p.OtherPath != "a.go" || p.Lines < dupWindow {
		t.Errorf("pair = %+v, want b.go copying a.go", p)
	}
	var out bytes.Buffer
	writeDupEvidence(&out, pairs)
	if !strings.Contains(out.String(), "change together") || !strings.Contains(out.String(), "b.go:") {
		t.Errorf("evidence should name the location and ask the change-together question:\n%s", out.String())
	}
}

func TestDupEvidence_IgnoresUnchangedCopies(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{
		"a.go": "package p\n\n" + dupSourceBlock,
		"b.go": "package q\n\n" + dupSourceBlock,
	})
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc unique() int { return 42 }\n")

	if pairs := scanDupEvidence(dir, resolveSizeBase(dir, "main")); len(pairs) != 0 {
		t.Fatalf("legacy copies outside the change must not be reported: %+v", pairs)
	}
}

// TestReviewDesignQuestions pins the review prompt from GitHub #312.
func TestReviewDesignQuestions(t *testing.T) {
	var out bytes.Buffer
	writeDesignQuestions(&out)
	for _, want := range []string{"design decision", "how many modules change"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("review questions missing %q:\n%s", want, out.String())
		}
	}
}

// TestDupEvidence_IgnoresImportBlocks: every Go file repeats a package line
// and an import list; that is not shared knowledge and must not be reported.
func TestDupEvidence_IgnoresImportBlocks(t *testing.T) {
	header := "package p\n\nimport (\n\t\"fmt\"\n\t\"io\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"sort\"\n\t\"strings\"\n)\n\n"
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{"a.go": header + "func a() int { return 1 }\n"})
	writeRepoFile(t, dir, "b.go", header+"func b() int { return 2 }\n")
	if pairs := scanDupEvidence(dir, resolveSizeBase(dir, "main")); len(pairs) != 0 {
		t.Fatalf("import blocks must not count as duplication: %+v", pairs)
	}
}
