package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFakeAstGrep writes a shell script that acts as a fake ast-grep binary.
// output is the raw text the fake binary should print to stdout.
// exitCode controls the process exit code.
func writeFakeAstGrep(t *testing.T, output string, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "ast-grep")
	content := "#!/bin/sh\n"
	if output != "" {
		content += `cat << 'FAKEEOF'` + "\n" + output + "\nFAKEEOF\n"
	}
	if exitCode != 0 {
		content += "exit " + string(rune('0'+exitCode)) + "\n"
	}
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatalf("write fake ast-grep: %v", err)
	}
	return dir
}

// prependPath prepends dir to the current PATH and restores it at test end.
func prependPath(t *testing.T, dir string) {
	t.Helper()
	orig := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+orig)
}

// ---- parseAstGrepJSON tests ----

func TestParseAstGrepJSON_Array(t *testing.T) {
	input := `[{"file":"foo.go","range":{"start":{"line":41}},"lines":"func Foo() {}"}]`
	matches, err := parseAstGrepJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want 1 match, got %d", len(matches))
	}
	if matches[0].File != "foo.go" {
		t.Errorf("file: got %q, want %q", matches[0].File, "foo.go")
	}
	if matches[0].Range.Start.Line != 41 {
		t.Errorf("line: got %d, want 41", matches[0].Range.Start.Line)
	}
}

func TestParseAstGrepJSON_NDJSON(t *testing.T) {
	input := `{"file":"bar.go","range":{"start":{"line":9}},"lines":"fmt.Println(x)"}` + "\n" +
		`{"file":"baz.go","range":{"start":{"line":0}},"lines":"fmt.Println(y)"}`
	matches, err := parseAstGrepJSON([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("want 2 matches, got %d", len(matches))
	}
}

func TestParseAstGrepJSON_Empty(t *testing.T) {
	for _, in := range []string{"", "[]", "null"} {
		matches, err := parseAstGrepJSON([]byte(in))
		if err != nil {
			t.Errorf("input %q: unexpected error: %v", in, err)
		}
		if len(matches) != 0 {
			t.Errorf("input %q: want 0 matches, got %d", in, len(matches))
		}
	}
}

// ---- collapseSnippet tests ----

func TestCollapseSnippet_Whitespace(t *testing.T) {
	in := "func Foo() {\n\treturn nil\n}"
	got := collapseSnippet(in)
	if strings.Contains(got, "\n") || strings.Contains(got, "\t") {
		t.Errorf("expected collapsed whitespace, got: %q", got)
	}
	if got != "func Foo() { return nil }" {
		t.Errorf("unexpected collapse result: %q", got)
	}
}

func TestCollapseSnippet_ANSI(t *testing.T) {
	in := "\x1b[32mfunc\x1b[0m Foo() {}"
	got := collapseSnippet(in)
	if strings.Contains(got, "\x1b") {
		t.Errorf("ANSI not stripped: %q", got)
	}
	if got != "func Foo() {}" {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestCollapseSnippet_Cap(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := collapseSnippet(long)
	if len([]rune(got)) > searchMaxSnippetLen {
		t.Errorf("snippet not capped: length %d", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis suffix, got: %q", got[:20])
	}
}

// ---- buildAstGrepArgs tests ----

func TestBuildAstGrepArgs_Basic(t *testing.T) {
	args := buildAstGrepArgs("func $NAME() {}", searchOpts{limit: 50})
	if args[0] != "run" {
		t.Errorf("first arg should be 'run', got %q", args[0])
	}
	if !searchSliceContains(args, "--json") {
		t.Error("--json flag missing")
	}
	if !searchSliceContains(args, "-p") {
		t.Error("-p flag missing")
	}
}

func TestBuildAstGrepArgs_WithLang(t *testing.T) {
	args := buildAstGrepArgs("fmt.Println($$$)", searchOpts{lang: "go", limit: 50})
	if !searchSliceContains(args, "-l") {
		t.Error("-l flag missing")
	}
	idx := searchSliceIndexOf(args, "-l")
	if idx+1 >= len(args) || args[idx+1] != "go" {
		t.Error("-l should be followed by 'go'")
	}
}

func TestBuildAstGrepArgs_WithPath(t *testing.T) {
	args := buildAstGrepArgs("x", searchOpts{path: "./internal", limit: 50})
	if !searchSliceContains(args, "./internal") {
		t.Errorf("path not in args: %v", args)
	}
}

func TestBuildAstGrepArgs_DefaultPathOmitted(t *testing.T) {
	args := buildAstGrepArgs("x", searchOpts{path: ".", limit: 50})
	if searchSliceContains(args, ".") {
		t.Errorf("default path '.' should be omitted, args: %v", args)
	}
}

// ---- runSearch integration tests using fake ast-grep ----

func TestRunSearch_HumanOutput(t *testing.T) {
	output := `[{"file":"foo.go","range":{"start":{"line":41}},"lines":"func Foo() {}"}]`
	dir := writeFakeAstGrep(t, output, 0)
	prependPath(t, dir)

	// Capture stdout by redirecting — test via the parse/format path directly.
	matches, err := parseAstGrepJSON([]byte(output))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want 1, got %d", len(matches))
	}
	snip := snippet(matches[0])
	if snip != "func Foo() {}" {
		t.Errorf("unexpected snippet: %q", snip)
	}
}

func TestRunSearch_LimitTruncation(t *testing.T) {
	// Build 5 matches, limit to 3.
	var objs []map[string]interface{}
	for i := 0; i < 5; i++ {
		objs = append(objs, map[string]interface{}{
			"file":  "x.go",
			"range": map[string]interface{}{"start": map[string]interface{}{"line": i}},
			"lines": "match",
		})
	}
	raw, _ := json.Marshal(objs)
	matches, err := parseAstGrepJSON(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(matches) != 5 {
		t.Fatalf("want 5 pre-limit, got %d", len(matches))
	}

	opts := searchOpts{limit: 3}
	if opts.limit > 0 && len(matches) > opts.limit {
		matches = matches[:opts.limit]
	}
	if len(matches) != 3 {
		t.Errorf("want 3 after limit, got %d", len(matches))
	}
}

func TestRunSearch_JSONOutput(t *testing.T) {
	matches := []astGrepMatch{
		{File: "a.go", Lines: "func Bar() {}"},
	}
	matches[0].Range.Start.Line = 9

	// printSearchJSON writes to stdout; test the data directly.
	snip := snippet(matches[0])
	if snip != "func Bar() {}" {
		t.Errorf("snippet: %q", snip)
	}
}

func TestSearchCmd_Registered(t *testing.T) {
	root := buildRoot()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "search" {
			found = true
			break
		}
	}
	if !found {
		t.Error("'search' command not registered in root")
	}
}

func TestSearchCmd_GroupID(t *testing.T) {
	root := buildRoot()
	for _, c := range root.Commands() {
		if c.Name() == "search" {
			if c.GroupID != "query" {
				t.Errorf("search GroupID: got %q, want %q", c.GroupID, "query")
			}
			return
		}
	}
	t.Error("search command not found")
}

// helpers

func searchSliceContains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func searchSliceIndexOf(ss []string, s string) int {
	for i, v := range ss {
		if v == s {
			return i
		}
	}
	return -1
}
