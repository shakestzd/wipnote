package oneshot

import (
	"fmt"
	"strings"
	"testing"
)

func lines(n int, prefix string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "%s%d\n", prefix, i)
	}
	return b.String()
}

// The mod's report: a status line, then the answer capped at maxReturnLines
// with one line naming the file holding the rest, then the run page.
func TestReport_CapsSections(t *testing.T) {
	in := ReportInput{Name: "codex", Dir: "/r", Exit: 0, Seconds: 7, Usage: "input 10 (cached 0), output 5",
		Last: lines(70, "a"), CleanLog: lines(300, "l"), TailLines: 200, MaxLines: 60, Page: "/r/run.html"}
	got := Report(in)
	want := "codex exited 0 after 7s, input 10 (cached 0), output 5; raw log /r/raw.log, answer file /r/last.md\n" +
		lines(60, "a") + "... 10 more lines in /r/last.md\n" + "run page: /r/run.html\n"
	if got != want {
		t.Fatalf("answer capped:\n%s\nwant\n%s", got, want)
	}
	// at or under the cap the answer is whole, its missing newline added
	in.Last, in.Page, in.Usage = "one\ntwo", "", ""
	if got := Report(in); got != "codex exited 0 after 7s; raw log /r/raw.log, answer file /r/last.md\none\ntwo\n" {
		t.Fatalf("whole answer: %q", got)
	}
	// 0 means no cap
	in.Last, in.MaxLines = lines(70, "a"), 0
	if got := Report(in); !strings.HasSuffix(got, lines(70, "a")) || strings.Contains(got, "more lines") {
		t.Fatalf("no cap: %q", got)
	}
}

// On failure, or with no answer, the end of clean.log: its last tailLines
// lines, of which the last maxReturnLines, led by how many earlier ones
// clean.log holds.
func TestReport_FailureTail(t *testing.T) {
	in := ReportInput{Name: "claude", Dir: "/r", Exit: 3, Seconds: 1, Last: "ignored on failure\n",
		CleanLog: lines(300, "l"), TailLines: 200, MaxLines: 60}
	got := Report(in)
	want := "claude exited 3 after 1s; raw log /r/raw.log, answer file /r/last.md\n" +
		"... 240 earlier lines in /r/clean.log\n" + strings.TrimPrefix(lines(300, "l"), lines(240, "l"))
	if got != want {
		t.Fatalf("tail:\n%s\nwant\n%s", got, want)
	}
	in.Exit, in.Last, in.CleanLog = 0, "", "only\nlines"
	if got := Report(in); !strings.HasSuffix(got, "answer file /r/last.md\nonly\nlines\n") {
		t.Fatalf("empty answer falls back to the log: %q", got)
	}
}

// ccx keeps the mod's stdout route: its own stdout (capped), its stderr on
// stderr, then the run page; no status line.
func TestReport_CcxStdoutRoute(t *testing.T) {
	out, errOut := CcxReport(CcxReportInput{Dir: "/r", Out: lines(5, "o"), Err: "warn", MaxLines: 3, Page: "/r/run.html"})
	if out != lines(3, "o")+"... 2 more lines in /r/out.txt\nrun page: /r/run.html\n" || errOut != "warn\n" {
		t.Fatalf("ccx report: %q / %q", out, errOut)
	}
}

func TestUsageCounts(t *testing.T) {
	in, cached, out, ok := UsageCounts("input 12,345 (cached 1,000), output 678, reasoning 9")
	if !ok || in != 12345 || cached != 1000 || out != 678 {
		t.Fatalf("codex usage: %d %d %d %v", in, cached, out, ok)
	}
	in, cached, out, ok = UsageCounts("4 in / 111 out tokens, $0.1077")
	if !ok || in != 4 || cached != 0 || out != 111 {
		t.Fatalf("claude usage: %d %d %d %v", in, cached, out, ok)
	}
	if _, _, _, ok := UsageCounts("12,000 tokens"); ok {
		t.Fatal("a review's total has no split")
	}
}
