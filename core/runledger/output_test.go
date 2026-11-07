package runledger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		{"error mid-line", "step 1\nbuild: error: linker failed\n", "build: error: linker failed", "build: error: linker failed"},
		{"traceback", "run\nTraceback (most recent call last):\n", "Traceback (most recent call last):", "Traceback (most recent call last):"},
		{"missing tool", "sh: jq: command not found\n", "sh: jq: command not found", "sh: jq: command not found"},
		{"colon but not file:line", "time: 12:30 done\nnote.v2: fine\n", "note.v2: fine", ""},
		{"errors word without colon", "no errors\nerror handling ok\n", "error handling ok", ""},
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

// A large clean log must summarise fast: the hooks summarise every output
// file a notification names, on the prompt path. The bound is relative, not
// wall-clock: the file exceeds the head window, so SummarizeFile must beat a
// full read-and-scan of the same file (the regression it guards) by a wide
// margin. Both sides are timed in this test and compared by best-of-N, so
// machine load scales them together.
func TestSummarizeFile_FastOnLargeCleanLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.output")
	os.WriteFile(path, []byte(strings.Repeat("ok  github.com/x/y/pkg 0.123s\n", 200000)), 0o644)
	best := func(fn func()) time.Duration {
		min := time.Duration(1<<63 - 1)
		for i := 0; i < 15; i++ {
			start := time.Now()
			fn()
			if d := time.Since(start); d < min {
				min = d
			}
		}
		return min
	}
	fullScan := best(func() {
		data, _ := os.ReadFile(path)
		lastLine(data)
		firstError(data)
	})
	bounded := best(func() { SummarizeFile(path) })
	if bounded*3 > fullScan {
		t.Errorf("SummarizeFile took %v on a 6MB clean log; a full scan takes %v, want under a third", bounded, fullScan)
	}
}

func TestSummarizeText_TruncatesLongLines(t *testing.T) {
	last, _ := SummarizeText(strings.Repeat("y", 5000))
	if len(last) > maxLineLen+len("…") {
		t.Errorf("last line not bounded: %d bytes", len(last))
	}
}
