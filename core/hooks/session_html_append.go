package hooks

import (
	"bytes"
	"errors"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// onSessionHTMLRead, when set, is told how many bytes each append read. It is
// a test seam (feat-c30d0792); nil in production.
var onSessionHTMLRead func(n int)

func noteSessionHTMLRead(n int) {
	if onSessionHTMLRead != nil {
		onSessionHTMLRead(n)
	}
}

// sessionHTMLTailWindow is how much of the file's end an append reads to find
// the activity log's closing </ol>. Everything after it is the closing
// section/article/body markup, a few hundred bytes.
const sessionHTMLTailWindow = 8 * 1024

// AppendEventToSessionHTML appends a <li> element to the session's HTML
// activity log, just before the last </ol>. It holds an exclusive flock for
// the whole update, preventing lost updates from concurrent hook invocations.
// Errors are silently logged (non-critical path).
//
// Only the file's tail is read and rewritten (feat-c30d0792): the <li>, the
// indentation and the old tail are written in place from the marker's offset.
// The file only grows, so nothing is truncated, and the bytes on disk are
// exactly what a full read-modify-rewrite produced — but the cost no longer
// grows with the session's length.
func AppendEventToSessionHTML(projectDir, sessionID string, ev SessionEvent) {
	htmlPath := filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html")

	f, err := os.OpenFile(htmlPath, os.O_RDWR, 0o644)
	if err != nil {
		// File doesn't exist — silently ignore (non-critical).
		return
	}
	defer f.Close()

	// Exclusive lock — blocks until no other process holds the lock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		debugLog(projectDir, "[session-html] flock %s: %v", htmlPath, err)
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	off, tail, err := lastMarkerTail(f, "</ol>")
	if err != nil {
		debugLog(projectDir, "[session-html] read %s: %v", htmlPath, err)
		return
	}
	if off < 0 {
		debugLog(projectDir, "[session-html] no </ol> marker in %s", htmlPath)
		return
	}
	insert := sessionEventLI(ev) + "            " + tail
	if _, err := f.WriteAt([]byte(insert), off); err != nil {
		debugLog(projectDir, "[session-html] write %s: %v", htmlPath, err)
	}
}

// lastMarkerTail returns the offset of the last occurrence of marker in f and
// the file's bytes from there to EOF, or -1 when marker is absent. It reads the
// last sessionHTMLTailWindow bytes and only falls back to the whole file when
// the marker is not in that window. A last occurrence inside the window is the
// file's last occurrence, so the answer equals strings.LastIndex on the whole
// content either way.
func lastMarkerTail(f *os.File, marker string) (int64, string, error) {
	info, err := f.Stat()
	if err != nil {
		return 0, "", err
	}
	start := max(info.Size()-sessionHTMLTailWindow, 0)
	buf, err := readSessionHTMLRange(f, start, info.Size())
	if err != nil {
		return 0, "", err
	}
	if i := bytes.LastIndex(buf, []byte(marker)); i >= 0 {
		return start + int64(i), string(buf[i:]), nil
	}
	if start == 0 {
		return -1, "", nil
	}
	buf, err = readSessionHTMLRange(f, 0, info.Size())
	if err != nil {
		return 0, "", err
	}
	i := bytes.LastIndex(buf, []byte(marker))
	if i < 0 {
		return -1, "", nil
	}
	return int64(i), string(buf[i:]), nil
}

// readSessionHTMLRange reads f[from:to].
func readSessionHTMLRange(f *os.File, from, to int64) ([]byte, error) {
	buf := make([]byte, to-from)
	n, err := f.ReadAt(buf, from)
	noteSessionHTMLRead(n)
	if err != nil && !(errors.Is(err, io.EOF) && n == len(buf)) {
		return nil, err
	}
	return buf[:n], nil
}

// sessionEventLI renders one activity-log entry, newline-terminated.
func sessionEventLI(ev SessionEvent) string {
	successStr := "true"
	if !ev.Success {
		successStr = "false"
	}
	var li strings.Builder
	li.WriteString(`                <li data-ts="`)
	li.WriteString(ev.Timestamp.UTC().Format(time.RFC3339))
	li.WriteString(`" data-tool="`)
	li.WriteString(html.EscapeString(ev.ToolName))
	li.WriteString(`" data-success="`)
	li.WriteString(successStr)
	li.WriteString(`" data-event-id="`)
	li.WriteString(html.EscapeString(ev.EventID))
	li.WriteString(`"`)
	writeOptionalAttr(&li, "data-feature", ev.FeatureID)
	writeOptionalAttr(&li, "data-status", ev.Status)
	writeOptionalAttr(&li, "data-reason", ev.Reason)
	li.WriteString(`>`)
	li.WriteString(html.EscapeString(ev.Summary))
	li.WriteString("</li>\n")
	return li.String()
}

// writeOptionalAttr writes ` name="value"` (escaped) when value is set.
func writeOptionalAttr(b *strings.Builder, name, value string) {
	if value == "" {
		return
	}
	b.WriteString(" " + name + `="`)
	b.WriteString(html.EscapeString(value))
	b.WriteString(`"`)
}
