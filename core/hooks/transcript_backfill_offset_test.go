package hooks

import (
	"os"
	"testing"
)

// TestStop_BackfillReadsFromOffset pins feat-c30d0792: the Stop hook's user
// prompt backfill resumes where the previous Stop left off, so a second Stop
// reads only the bytes appended since — and still recovers the new prompt.
func TestStop_BackfillReadsFromOffset(t *testing.T) {
	td := setupTestDB(t)
	projectDir := t.TempDir()
	path := writeUserTranscript(t, []string{
		makeUserPromptLine("off-1", "", "test-sess", "first", false),
		makeUserPromptLine("off-2", "off-1", "test-sess", "second", false),
	})
	if n, err := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path); err != nil || n != 2 {
		t.Fatalf("first backfill: n=%d err=%v, want 2", n, err)
	}

	newLine := makeUserPromptLine("off-3", "off-2", "test-sess", "third", false)
	appendTranscriptLine(t, path, newLine)
	read := 0
	onBackfillRead = func(n int) { read += n }
	t.Cleanup(func() { onBackfillRead = nil })

	n, err := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path)
	if err != nil || n != 1 {
		t.Fatalf("second backfill: n=%d err=%v, want the 1 new prompt", n, err)
	}
	if want := len(newLine) + 1; read != want {
		t.Errorf("second backfill read %d bytes, want only the %d appended", read, want)
	}
}

// TestStop_BackfillRescansRewrittenTranscript: a transcript that shrank (or
// was replaced) is not trusted to the saved offset; the scan starts over.
func TestStop_BackfillRescansRewrittenTranscript(t *testing.T) {
	td := setupTestDB(t)
	projectDir := t.TempDir()
	path := writeUserTranscript(t, []string{
		makeUserPromptLine("rw-1", "", "test-sess", "one", false),
		makeUserPromptLine("rw-2", "rw-1", "test-sess", "two", false),
	})
	if _, err := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path); err != nil {
		t.Fatal(err)
	}
	short := makeUserPromptLine("rw-new", "", "test-sess", "fresh", false) + "\n"
	if err := os.WriteFile(path, []byte(short), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path); err != nil || n != 1 {
		t.Fatalf("rewritten transcript: n=%d err=%v, want 1", n, err)
	}
}

// TestStop_BackfillRetriesUnterminatedLine: a last line still being written
// (no newline yet) is not counted as consumed, so the next Stop reads it again
// once it is complete.
func TestStop_BackfillRetriesUnterminatedLine(t *testing.T) {
	td := setupTestDB(t)
	projectDir := t.TempDir()
	path := writeUserTranscript(t, []string{makeUserPromptLine("ut-1", "", "test-sess", "one", false)})
	full := makeUserPromptLine("ut-2", "ut-1", "test-sess", "two", false)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString(full[:len(full)/2])
	f.Close()
	if n, _ := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path); n != 1 {
		t.Fatalf("partial line: n=%d, want 1", n)
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	_, _ = f.WriteString(full[len(full)/2:] + "\n")
	f.Close()
	read := 0
	onBackfillRead = func(n int) { read += n }
	t.Cleanup(func() { onBackfillRead = nil })
	if n, err := backfillMissedUserPrompts(td.DB, projectDir, "test-sess", path); err != nil || n != 1 {
		t.Fatalf("completed line: n=%d err=%v, want 1", n, err)
	}
	if read != len(full)+1 {
		t.Errorf("resumed scan read %d bytes, want %d (from the start of the once-partial line)", read, len(full)+1)
	}
}
