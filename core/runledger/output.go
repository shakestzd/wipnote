package runledger

import (
	"bytes"
	"io"
	"os"
	"strings"
)

// Output-file read bounds: the first error is looked for in the head, the
// last line in the tail, so a huge log costs two bounded reads.
const (
	outputHeadBytes = 1 << 20
	outputTailBytes = 16 << 10
)

// errorPrefixes, errorInfixes and errorSuffixes are the line shapes that
// mark a failure in the logs agents run in the background: go test/vet/build,
// panics, shell and compiler errors. "0 errors" and similar summaries do not
// match. Plain string checks, not a regexp: the matcher runs on every line of
// every output file a notification names, on the synchronous prompt path.
var (
	errorPrefixes = []string{"--- FAIL", "panic:", "fatal:", "Error:", "error:", "E: ", "npm ERR!"}
	errorWords    = []string{"FAIL", "FATAL", "ERROR"}
	errorInfixes  = []string{"error: ", "Error: ", "ERROR: ", "Traceback (most recent call last)"}
	errorSuffixes = []string{": command not found"}
)

// isErrorLine reports whether a trimmed, non-empty line is an error line.
func isErrorLine(s string) bool {
	for _, p := range errorPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	for _, w := range errorWords {
		if strings.HasPrefix(s, w) && (len(s) == len(w) || !isWordByte(s[len(w)])) {
			return true
		}
	}
	for _, p := range errorInfixes {
		if strings.Contains(s, p) {
			return true
		}
	}
	for _, p := range errorSuffixes {
		if strings.HasSuffix(s, p) {
			return true
		}
	}
	return isFileLineRef(s)
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// isFileLineRef matches a compiler location lead, "path/file.go:12:" or
// "file.go:12:3:" followed by a space.
func isFileLineRef(s string) bool {
	sp := strings.IndexByte(s, ' ')
	if sp < 2 || s[sp-1] != ':' {
		return false
	}
	parts := strings.Split(s[:sp-1], ":")
	if len(parts) < 2 || len(parts) > 3 || !strings.Contains(parts[0], ".") {
		return false
	}
	for _, n := range parts[1:] {
		if n == "" || strings.Trim(n, "0123456789") != "" {
			return false
		}
	}
	return true
}

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
		if s != "" && isErrorLine(s) {
			return Truncate(s, maxLineLen)
		}
	}
	return ""
}
