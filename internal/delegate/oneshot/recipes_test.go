package oneshot

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func mustClassify(t *testing.T, argv ...string) Recipe {
	t.Helper()
	r, err := Classify(argv)
	if err != nil {
		t.Fatalf("Classify(%q): %v", argv, err)
	}
	return r
}

func TestRecipes_Classify(t *testing.T) {
	cases := []struct {
		argv []string
		want Kind
	}{
		{[]string{"codex", "exec", "list files"}, KindCodex},
		{[]string{"codex", "e", "x"}, KindCodex},
		{[]string{"/usr/local/bin/codex", "review", "--base", "main"}, KindCodexReview},
		{[]string{"claude", "-p", "hi"}, KindClaude},
		{[]string{"claude", "--model", "haiku", "--print", "hi"}, KindClaude},
		{[]string{"ccx", "task", "fix it"}, KindCcx},
	}
	for _, c := range cases {
		if got := mustClassify(t, c.argv...).Kind; got != c.want {
			t.Errorf("%q: kind %q, want %q", c.argv, got, c.want)
		}
	}
	for _, bad := range [][]string{
		{"codex", "login"}, {"claude", "hi"}, {"claude", "mcp", "list", "-p"}, {"ls", "-la"},
		{"ccx", "shell"}, {"codex", "exec", "--help"}, {"claude", "-p", "--version"}, {},
		{"claude", "-p", "--output-format", "json", "hi"},
	} {
		if _, err := Classify(bad); err == nil {
			t.Errorf("Classify(%q) accepted a non-run", bad)
		}
	}
	// a prompt after -- may hold any words, flags included
	mustClassify(t, "claude", "-p", "--", "explain --help")
}

// Chained commands stay v2 (q-chained): shell syntax is refused, never run.
func TestRecipes_RefusesChained(t *testing.T) {
	for _, argv := range [][]string{
		{"codex", "exec", "x", "|", "sort"},
		{"codex", "exec", "x", ";", "ls"},
		{"codex", "review", ">", "v.txt"},
		{"claude", "-p", "x", "&&", "echo"},
		{`codex exec "x" | sort`},
	} {
		_, err := Classify(argv)
		if !errors.Is(err, ErrChained) {
			t.Errorf("Classify(%q) = %v, want ErrChained", argv, err)
		}
	}
	// a prompt that merely mentions a pipe is one argument and runs
	mustClassify(t, "codex", "exec", "is a | b right?")
}

func TestRecipes_LeanProfileOnlyWhenPresent(t *testing.T) {
	home := t.TempDir()
	o := CommandOptions{LastFile: "/run/last.md", LeanProfile: "lean", CodexHome: home}
	r := mustClassify(t, "codex", "exec", "x")
	if got := r.Command(o); slices.Contains(got, "-p") {
		t.Fatalf("no lean.config.toml, yet -p was added: %q", got)
	}
	if err := os.WriteFile(filepath.Join(home, "lean.config.toml"), []byte("model = \"m\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "--output-last-message", "/run/last.md", "-p", "lean", "--json", "x"}
	if got := r.Command(o); !slices.Equal(got, want) {
		t.Fatalf("Command = %q, want %q", got, want)
	}
	// the caller's own profile wins; the option off adds none; review has no profile flag
	if got := mustClassify(t, "codex", "exec", "--profile=deep", "x").Command(o); slices.Contains(got, "lean") {
		t.Errorf("own profile overridden: %q", got)
	}
	if got := r.Command(CommandOptions{LastFile: "/l", CodexHome: home}); slices.Contains(got, "-p") {
		t.Errorf("leanProfile off, yet -p: %q", got)
	}
	if got := mustClassify(t, "codex", "review").Command(o); !slices.Equal(got, []string{"codex", "review"}) {
		t.Errorf("review changed: %q", got)
	}
}

func TestRecipes_CodexFlagsOnlyWhenAbsent(t *testing.T) {
	o := CommandOptions{LastFile: "/run/last.md"}
	got := mustClassify(t, "codex", "exec", "--json", "-o", "mine.md", "x").Command(o)
	if !slices.Equal(got, []string{"codex", "exec", "--json", "-o", "mine.md", "x"}) {
		t.Errorf("Command = %q", got)
	}
	for _, spelling := range [][]string{{"-o", "f.md"}, {"-of.md"}, {"-o=f.md"}, {"--output-last-message", "f.md"}, {"--output-last-message=f.md"}} {
		r := mustClassify(t, slices.Concat([]string{"codex", "exec"}, spelling, []string{"x"})...)
		if f, ok := r.OwnLastFile(); !ok || f != "f.md" {
			t.Errorf("%q: own file %q %v", spelling, f, ok)
		}
	}
	if _, ok := mustClassify(t, "codex", "exec", "x", "--", "-o y").OwnLastFile(); ok {
		t.Error("a -o after -- is prompt text, not a flag")
	}
}

func TestRecipes_ClaudeStreamJSON(t *testing.T) {
	got := mustClassify(t, "claude", "-p", "hi").Command(CommandOptions{})
	if !slices.Equal(got, []string{"claude", "--output-format", "stream-json", "--verbose", "-p", "hi"}) {
		t.Errorf("Command = %q", got)
	}
	got = mustClassify(t, "claude", "--verbose", "--output-format=stream-json", "-p", "hi").Command(CommandOptions{})
	if !slices.Equal(got, []string{"claude", "--verbose", "--output-format=stream-json", "-p", "hi"}) {
		t.Errorf("own flags duplicated: %q", got)
	}
}

func TestRecipes_DerivedLabel(t *testing.T) {
	cases := map[string][]string{
		"cdx: list the files here":         {"codex", "exec", "-C", "/w", "list the files here"},
		"cdx review: changes against main": {"codex", "review", "--base", "main"},
		"cdx review: current changes":      {"codex", "review"},
		"Claude task: say hi":              {"claude", "-p", "say hi", "--model", "haiku"},
		"Claude review: /code-review 12":   {"claude", "-p", "/code-review 12"},
		"ccx task: fix the build":          {"ccx", "task", "fix the build"},
		"cdx: rm -rf x and echo hi":        {"codex", "exec", "rm -rf $(x) and `echo` hi"},
		"cdx: " + strings.Repeat("a", 48):  {"codex", "exec", strings.Repeat("a", 60) + " b"},
	}
	for want, argv := range cases {
		if got := mustClassify(t, argv...).DerivedLabel(); got != want {
			t.Errorf("%q: label %q, want %q", argv, got, want)
		}
	}
}
