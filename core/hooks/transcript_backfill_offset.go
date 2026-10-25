package hooks

import (
	"bufio"
	"bytes"
	"database/sql"
	"os"
	"strconv"
)

// onBackfillRead, when set, is told how many transcript bytes each scanned
// line consumed. It is a test seam (feat-c30d0792); nil in production.
var onBackfillRead func(n int)

func noteBackfillRead(n int) {
	if onBackfillRead != nil {
		onBackfillRead(n)
	}
}

// lineTracker is a bufio.ScanLines split that also records how many bytes the
// last token consumed and whether a newline ended it, so a scan can tell how
// far into the file its complete lines reach.
type lineTracker struct {
	last       int  // bytes the last token consumed, newline included
	terminated bool // the last token ended with '\n' (not EOF)
}

func (l *lineTracker) split(data []byte, atEOF bool) (int, []byte, error) {
	adv, tok, err := bufio.ScanLines(data, atEOF)
	if tok != nil || adv > 0 {
		l.last = adv
		l.terminated = bytes.IndexByte(data[:adv], '\n') >= 0
	}
	return adv, tok, err
}

// backfillOffsetKey names the metadata row holding how far the user-prompt
// backfill has read transcriptPath for sessionID. The offset lives in the same
// database as the rows it vouches for, so a fresh database rescans from 0.
func backfillOffsetKey(sessionID, transcriptPath string) string {
	return "transcript_backfill_offset:" + sessionID + ":" + transcriptPath
}

// loadBackfillOffset returns the saved offset for key, or 0 when there is
// none or it cannot be read.
func loadBackfillOffset(database *sql.DB, key string) int64 {
	var value string
	if err := database.QueryRow(`SELECT value FROM metadata WHERE key = ?`, key).Scan(&value); err != nil {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// resumeOffset returns saved when it still marks the end of a complete line in
// f, else 0. A transcript that shrank or no longer has a newline just before
// the offset was rewritten, and is rescanned from the start.
func resumeOffset(f *os.File, saved int64) int64 {
	if saved <= 0 {
		return 0
	}
	info, err := f.Stat()
	if err != nil || saved > info.Size() {
		return 0
	}
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, saved-1); err != nil || b[0] != '\n' {
		return 0
	}
	return saved
}

// saveBackfillOffset records that the transcript is consumed through to. It
// writes only when the offset moved. Failure is logged and harmless: the next
// run rescans more than it needs to, and the inserts are idempotent.
func saveBackfillOffset(database *sql.DB, projectDir, key string, from, to int64) {
	if to == from {
		return
	}
	if _, err := database.Exec(`
		INSERT INTO metadata (key, value, updated_at) VALUES (?, ?, datetime('now'))
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, strconv.FormatInt(to, 10)); err != nil {
		debugLog(projectDir, "[user-prompt-backfill] save offset: %v", err)
	}
}
