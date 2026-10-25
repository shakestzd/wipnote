package oneshot

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// The report is the mod's (wrap.ts report, q-report): agents already read
// its status line and its answer-file pointer, so the format is kept byte
// for byte.

// ReportInput is what the codex and claude report reads.
type ReportInput struct {
	Name      string // codex | claude
	Dir       string // the run folder
	Exit      int
	Seconds   int64
	Usage     string
	Last      string // last.md
	CleanLog  string // clean.log
	TailLines int    // clean.log lines kept after a failure
	MaxLines  int    // maxReturnLines; 0 is no cap
	Page      string // run.html, empty when it was not written
}

// Report is the status line, then the answer when the run succeeded and
// wrote one, else the end of clean.log, then the run page.
func Report(in ReportInput) string {
	var b strings.Builder
	usage := ""
	if in.Usage != "" {
		usage = ", " + in.Usage
	}
	fmt.Fprintf(&b, "%s exited %d after %ds%s; raw log %s, answer file %s\n", in.Name, in.Exit, in.Seconds, usage,
		filepath.Join(in.Dir, runstore.FileRawLog), filepath.Join(in.Dir, runstore.FileLastMD))
	if in.Exit == 0 && in.Last != "" {
		b.WriteString(capFile(in.Last, in.MaxLines, filepath.Join(in.Dir, runstore.FileLastMD)))
	} else {
		tail := lastLines(in.CleanLog, max(1, in.TailLines))
		b.WriteString(capTail(tail, countLines(in.CleanLog), in.MaxLines, filepath.Join(in.Dir, runstore.FileCleanLog)))
	}
	b.WriteString(pageLine(in.Page))
	return b.String()
}

// CcxReportInput is what the ccx report reads.
type CcxReportInput struct {
	Dir      string
	Out, Err string // ccx's stdout and stderr
	MaxLines int
	Page     string
}

// CcxReport is what an unwrapped ccx run returns, its stdout (status line
// and result, capped) and its stderr, then the run page.
func CcxReport(in CcxReportInput) (stdout, stderr string) {
	stdout = capFile(in.Out, in.MaxLines, filepath.Join(in.Dir, runstore.FileOut)) + pageLine(in.Page)
	return stdout, withNewline(in.Err)
}

func pageLine(page string) string {
	if page == "" {
		return ""
	}
	return "run page: " + page + "\n"
}

// countLines counts as awk's NR does: a last line without its newline counts.
func countLines(s string) int {
	n := strings.Count(s, "\n")
	if s != "" && !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// splitKeep splits s into lines that keep their newlines.
func splitKeep(s string) []string {
	if s == "" {
		return nil
	}
	return strings.SplitAfter(strings.TrimSuffix(s, "\n"), "\n")
}

func withNewline(s string) string {
	if s != "" && !strings.HasSuffix(s, "\n") {
		return s + "\n"
	}
	return s
}

// lastLines is tail -n n.
func lastLines(s string, n int) string {
	ls := splitKeep(s)
	if len(ls) <= n {
		return s
	}
	out := strings.Join(ls[len(ls)-n:], "")
	if strings.HasSuffix(s, "\n") && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

// capFile is the mod's cap_file: the first max lines and one line naming
// the file that holds the rest; a section within the cap is returned whole.
func capFile(s string, maxLines int, file string) string {
	n := countLines(s)
	if maxLines <= 0 || n <= maxLines {
		return withNewline(s)
	}
	head := strings.Join(splitKeep(s)[:maxLines], "")
	return withNewline(head) + fmt.Sprintf("... %d more lines in %s\n", n-maxLines, file)
}

// capTail is the mod's cap_tail: the last max lines of tail, led by how
// many earlier lines the whole log (total lines) holds.
func capTail(tail string, total, maxLines int, file string) string {
	n := countLines(tail)
	if maxLines <= 0 || n <= maxLines {
		return withNewline(tail)
	}
	return fmt.Sprintf("... %d earlier lines in %s\n", total-maxLines, file) + withNewline(lastLines(tail, maxLines))
}

var (
	codexUsageRe  = regexp.MustCompile(`^input ([0-9,]+) \(cached ([0-9,]+)\), output ([0-9,]+)`)
	claudeUsageRe = regexp.MustCompile(`^([0-9,.e+]+) in / ([0-9,.e+]+) out tokens`)
)

// UsageCounts reads token counts back from a usage line (render's codex
// split or stream-json totals) for the ledger. ok is false for a line with
// no split, such as a review's bare total.
func UsageCounts(line string) (in, cached, out int, ok bool) {
	if m := codexUsageRe.FindStringSubmatch(line); m != nil {
		return atoi(m[1]), atoi(m[2]), atoi(m[3]), true
	}
	if m := claudeUsageRe.FindStringSubmatch(line); m != nil {
		return atoi(m[1]), 0, atoi(m[2]), true
	}
	return 0, 0, 0, false
}

func atoi(s string) int {
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, ",", ""), 64)
	if err != nil {
		return 0
	}
	return int(f)
}
