package runledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSummarizeText(t *testing.T) {
	cases := []struct {
		name, text, last, first string
	}{
		{"go test failure", "ok  core 0.1s\n--- FAIL: TestX (0.00s)\nFAIL\tcore\t0.2s\necho done\n", "echo done", "--- FAIL: TestX (0.00s)"},
		{"compile error", "# pkg\nmain.go:3:2: undefined: x\n", "main.go:3:2: undefined: x", "main.go:3:2: undefined: x"},
		{"panic", "starting\npanic: boom\n", "panic: boom", "panic: boom"},
		{"clean run", "ok  core 0.1s\n0 errors found\nPASS\n\n", "PASS", ""},
		{"empty", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			last, first := SummarizeText(c.text)
			if last != c.last || first != c.first {
				t.Errorf("SummarizeText = (%q, %q), want (%q, %q)", last, first, c.last, c.first)
			}
		})
	}
}

// The exit code is the outer command's: a gate ending in "echo" exits 0 even
// when a step failed, so the first error line is what tells the truth.
func TestSummarizeFile_KeepsFirstErrorAndLastLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b1.output")
	body := "== core\n--- FAIL: TestA (0.01s)\n" + strings.Repeat("noise line\n", 50000) + "gate finished\n"
	os.WriteFile(path, []byte(body), 0o644)
	last, first := SummarizeFile(path)
	if last != "gate finished" || first != "--- FAIL: TestA (0.01s)" {
		t.Errorf("SummarizeFile = (%q, %q)", last, first)
	}
	if l, f := SummarizeFile(filepath.Join(t.TempDir(), "missing")); l != "" || f != "" {
		t.Error("missing file must summarise to nothing")
	}
}

func TestSummarizeText_TruncatesLongLines(t *testing.T) {
	last, _ := SummarizeText(strings.Repeat("y", 5000))
	if len(last) > maxLineLen+len("…") {
		t.Errorf("last line not bounded: %d bytes", len(last))
	}
}
