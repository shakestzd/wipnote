package render

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain clears the variables that could point a test at a real project or
// a live herdr: every test here uses temp folders only.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "HERDR_") {
			_ = os.Unsetenv(name)
		}
	}
	_ = os.Unsetenv("WIPNOTE_PROJECT_DIR")
	_ = os.Unsetenv("CLAUDE_PROJECT_DIR")
	os.Exit(m.Run())
}

// golden reads a testdata file written by the retired herdr pane mod's jq
// and awk pipeline (kept as the reference since the mod was removed).
func golden(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("golden %s: %v", path, err)
	}
	return string(b)
}

func same(t *testing.T, what, got, want string) {
	t.Helper()
	if got == want {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := 0; i < len(gl) || i < len(wl); i++ {
		var g, w string
		if i < len(gl) {
			g = gl[i]
		}
		if i < len(wl) {
			w = wl[i]
		}
		if g != w {
			t.Errorf("%s differs at line %d:\n got: %q\nwant: %q", what, i+1, g, w)
			return
		}
	}
	t.Errorf("%s differs (trailing bytes): got %d bytes, want %d", what, len(got), len(want))
}

func inputs(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("no inputs for %s: %v", pattern, err)
	}
	return files
}

// Each Codex raw log (the three real runs and the cases ported from the mod's
// md_cases.py) renders the same clean.log, clean.md, last.md and usage as the
// mod's pipeline.
func TestRender_CodexFixtures(t *testing.T) {
	for _, raw := range inputs(t, "codex/*.raw") {
		base := strings.TrimSuffix(raw, ".raw")
		t.Run(filepath.Base(base), func(t *testing.T) {
			out := Codex([]byte(golden(t, raw)))
			same(t, "clean.log", out.CleanLog, golden(t, base+".clean.log"))
			same(t, "clean.md", out.CleanMD, golden(t, base+".clean.md"))
			same(t, "last.md", out.Last, golden(t, base+".last.md"))
			same(t, "usage", usageFile(out.Usage), golden(t, base+".usage"))
		})
	}
}

// usageFile is the usage file as the mod writes it: the line and a newline,
// or nothing.
func usageFile(line string) string {
	if line == "" {
		return ""
	}
	return line + "\n"
}

// The fixtures must include the four the plan names.
func TestRender_FixtureSet(t *testing.T) {
	for _, f := range []string{"codex/run1.raw", "codex/run2.raw", "codex/run3.raw", "claude/claude.stream.jsonl"} {
		if _, err := os.Stat(filepath.Join("testdata", f)); err != nil {
			t.Errorf("fixture %s missing: %v", f, err)
		}
	}
}

// A headless claude -p run (stream-json) renders the mod's clean.log (pane
// mode), clean.md, usage and last.md.
func TestRender_ClaudeStreamJSON(t *testing.T) {
	for _, log := range inputs(t, "claude/*.jsonl") {
		base := strings.TrimSuffix(log, ".jsonl")
		t.Run(filepath.Base(base), func(t *testing.T) {
			out := Claude([]byte(golden(t, log)))
			same(t, "clean.log", out.CleanLog, golden(t, base+".clean.log"))
			same(t, "clean.md", out.CleanMD, golden(t, base+".clean.md"))
			same(t, "usage", usageFile(out.Usage), golden(t, base+".usage"))
			same(t, "last.md", out.Last, golden(t, base+".last.md"))
		})
	}
}

// ccx: the stream-json log followed line by line. The live Markdown (calls
// still waiting are held back) is a prefix of the final one, so a follower
// only ever appends; results land under their own call in arrival order; and
// last.md quotes the status line before the answer.
func TestRender_CcxOrder(t *testing.T) {
	for _, log := range inputs(t, "claude/*.jsonl") {
		base := strings.TrimSuffix(log, ".jsonl")
		t.Run(filepath.Base(base), func(t *testing.T) {
			raw := golden(t, log)
			f := NewCcxFilter()
			var clean, md strings.Builder
			for _, line := range splitLines([]byte(raw)) {
				l, m := f.Line(line)
				clean.WriteString(l)
				md.WriteString(m)
			}
			same(t, "mdlive", md.String(), golden(t, base+".mdlive.md"))
			md.WriteString(f.Close())
			same(t, "view.log", clean.String(), golden(t, base+".view.log"))
			same(t, "clean.md", md.String(), golden(t, base+".clean.md"))
			same(t, "Ccx clean.md", Ccx([]byte(raw), nil).CleanMD, golden(t, base+".clean.md"))
		})
	}
	pairing := Ccx([]byte(golden(t, "testdata/claude/stream_pairing.jsonl")), nil).CleanMD
	if b, a := strings.Index(pairing, "R-b"), strings.Index(pairing, "R-a"); b < 0 || a < 0 || b > a {
		t.Errorf("results not in arrival order:\n%s", pairing)
	}
	out := Ccx(nil, []byte("ccx: done in 3s\nthe answer\nline 2\n"))
	if want := "> ccx: done in 3s\n\nthe answer\nline 2\n"; out.Last != want {
		t.Errorf("ccx last.md = %q, want %q", out.Last, want)
	}
	if got := Ccx(nil, nil).Last; got != "> \n\n" {
		t.Errorf("empty ccx stdout last.md = %q", got)
	}
}

// The streaming Codex filter, fed one complete line at a time, writes what
// the whole-log render writes.
func TestRender_CodexStreamingMatchesWhole(t *testing.T) {
	raw := []byte(golden(t, "testdata/codex/run1.raw"))
	f := NewCodexFilter()
	var log, md strings.Builder
	for _, line := range splitLines(raw) {
		l, m := f.Line(line)
		log.WriteString(l)
		md.WriteString(m)
	}
	l, m := f.Close()
	log.WriteString(l)
	md.WriteString(m)
	whole := Codex(raw)
	same(t, "clean.log", log.String(), whole.CleanLog)
	same(t, "clean.md", md.String(), whole.CleanMD)
}

// Redaction runs on RE2 (linear): orch's backtracking regexes took seconds
// on 8000 characters. A page from a hostile log four times as long takes
// about four times as long, not sixteen, and the secret is masked on it.
func TestRender_RedactLinearTime(t *testing.T) {
	timed := func(n int) time.Duration {
		hostile := strings.Repeat("token = ", n) + strings.Repeat("a", 2*n) +
			strings.Repeat(" Bearer ", n) + strings.Repeat("`*_<", n)
		md := "### Assistant\n\n" + hostile + "\n\napi_key=sk-abcdefghijklmnop\n"
		start := time.Now()
		page := Page(PageInput{Label: "l", Status: "s", OK: true, Parts: ParseRun(md, true), Answer: hostile, LogPath: "/d"})
		if strings.Contains(page, "sk-abcdefghijklmnop") {
			t.Error("the secret reached run.html unmasked")
		}
		return time.Since(start)
	}
	timed(500) // warm up
	small, large := timed(5000), timed(20000)
	if large > 10*small+50*time.Millisecond {
		t.Errorf("4x the input took %v against %v: not linear", large, small)
	}
}
