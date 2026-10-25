package hooks

import (
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
)

// countActivityEntries counts the number of <li …> elements inside
// <section data-activity-log> in the session HTML content. This is the
// canonical way to detect whether a live (non-finalized) session has recorded
// events: AppendEventToSessionHTML writes <li> entries but does NOT update
// data-event-count (only FinalizeSessionHTML does that). Returns 0 when the
// section is absent or contains no entries.
func countActivityEntries(content string) int {
	const sectionMarker = `<section data-activity-log>`
	sIdx := strings.Index(content, sectionMarker)
	if sIdx < 0 {
		return 0
	}
	section := content[sIdx:]
	return strings.Count(section, "<li ")
}

// articleAttrRe matches data attributes on the <article> tag for replacement.
var articleStatusRe = regexp.MustCompile(`data-status="[^"]*"`)

var articleEventCountRe = regexp.MustCompile(`data-event-count="[^"]*"`)

var badgeStatusRe = regexp.MustCompile(`<span class="badge status-[^"]*">[^<]*</span>`)

var badgeEventsRe = regexp.MustCompile(`<span class="badge">\d+ events?</span>`)

// FinalizeSessionHTML updates the session HTML file with completion data:
// sets data-status, adds data-ended-at, and updates data-event-count.
// Errors are silently logged. Acquires the same exclusive flock as
// AppendEventToSessionHTML so it does not race a concurrent sweep append
// or a late PostToolUse write during SessionEnd.
func FinalizeSessionHTML(projectDir, sessionID, endedAt, status string, eventCount int) {
	htmlPath := filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html")
	f, err := os.OpenFile(htmlPath, os.O_RDWR, 0o644)
	if err != nil {
		// File doesn't exist — silently ignore.
		return
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		debugLog(projectDir, "[session-html] finalize flock %s: %v", htmlPath, err)
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	data, err := io.ReadAll(f)
	if err != nil {
		debugLog(projectDir, "[session-html] finalize read %s: %v", htmlPath, err)
		return
	}

	content := string(data)

	// Update data-status.
	content = articleStatusRe.ReplaceAllString(content,
		fmt.Sprintf(`data-status="%s"`, html.EscapeString(status)))

	// Add data-ended-at after data-status on the article tag.
	// Find the article's data-status and insert data-ended-at after it.
	endedAtAttr := fmt.Sprintf(`data-ended-at="%s"`, html.EscapeString(endedAt))
	if !strings.Contains(content, "data-ended-at=") {
		// Insert after data-status in the article tag.
		statusAttr := fmt.Sprintf(`data-status="%s"`, html.EscapeString(status))
		content = strings.Replace(content, statusAttr,
			statusAttr+"\n             "+endedAtAttr, 1)
	}

	// Update data-event-count.
	content = articleEventCountRe.ReplaceAllString(content,
		fmt.Sprintf(`data-event-count="%d"`, eventCount))

	// Update badge status text.
	statusTitle := strings.ToUpper(status[:1]) + status[1:]
	content = badgeStatusRe.ReplaceAllString(content,
		fmt.Sprintf(`<span class="badge status-%s">%s</span>`,
			html.EscapeString(status), html.EscapeString(statusTitle)))

	// Update badge event count text.
	evtWord := "events"
	if eventCount == 1 {
		evtWord = "event"
	}
	content = badgeEventsRe.ReplaceAllString(content,
		fmt.Sprintf(`<span class="badge">%d %s</span>`, eventCount, evtWord))

	if err := f.Truncate(0); err != nil {
		debugLog(projectDir, "[session-html] finalize truncate %s: %v", htmlPath, err)
		return
	}
	if _, err := f.Seek(0, 0); err != nil {
		debugLog(projectDir, "[session-html] finalize seek %s: %v", htmlPath, err)
		return
	}
	if _, err := f.Write([]byte(content)); err != nil {
		debugLog(projectDir, "[session-html] finalize write %s: %v", htmlPath, err)
	}
}
