package runledger

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strings"
)

// Output-file read bounds: the first error is looked for in the head, the
// last line in the tail, so a huge log costs two bounded reads.
const (
	outputHeadBytes = 1 << 20
	outputTailBytes = 16 << 10
)

// errorLinePattern matches the line shapes that mark a failure in the logs
// agents run in the background: go test/vet/build, panics, shell and
// compiler errors. "0 errors" and similar summaries do not match.
var errorLinePattern = regexp.MustCompile(
	`^(--- FAIL|FAIL\b|panic:|fatal:|FATAL\b|ERROR\b|Error:|error:|E: )` +
		`|^\S+\.\w+:\d+(:\d+)?: ` +
		`|\b(error|Error|ERROR): ` +
		`|: command not found$|^npm ERR!|Traceback \(most recent call last\)`,
)

// SummarizeText returns the last non-empty line and the first error line of
// text, each cut to maxLineLen. The first error is "" when no line matches.
func SummarizeText(text string) (last, firstErr string) {
	return lastLine([]byte(text)), firstError([]byte(text))
}

// SummarizeFile is SummarizeText over an output file, read in bounded
// pieces. A missing or unreadable file summarises to nothing.
func SummarizeFile(path string) (last, firstErr string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return "", ""
	}
	head := readAt(f, 0, min(info.Size(), outputHeadBytes))
	tailStart := max(info.Size()-outputTailBytes, 0)
	tail := head
	if tailStart > 0 {
		tail = readAt(f, tailStart, info.Size()-tailStart)
	}
	return lastLine(tail), firstError(head)
}

func readAt(f *os.File, off, n int64) []byte {
	buf := make([]byte, n)
	got, err := f.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		return nil
	}
	return buf[:got]
}

func lastLine(buf []byte) string {
	lines := bytes.Split(bytes.TrimRight(buf, "\r\n \t"), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(string(lines[i])); s != "" {
			return Truncate(s, maxLineLen)
		}
	}
	return ""
}

func firstError(buf []byte) string {
	for _, line := range bytes.Split(buf, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if s != "" && errorLinePattern.MatchString(s) {
			return Truncate(s, maxLineLen)
		}
	}
	return ""
}
