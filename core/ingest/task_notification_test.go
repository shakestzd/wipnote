package ingest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const notificationFixture = "testdata/task_notifications/session.jsonl"

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(notificationFixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// The recorded fixture holds one shell, one subagent and one monitor end, plus
// lines that must NOT become ends: a remote-notification batch (no task id), a
// queue remove, a monitor event (no status) and the same agent notification
// delivered again as a user message.
func TestParseTaskNotifications_RecordedTranscript(t *testing.T) {
	got := ParseTaskNotifications(readFixture(t))
	if len(got) != 3 {
		t.Fatalf("got %d notifications, want 3: %+v", len(got), got)
	}
	shell, agent, monitor := got[0], got[1], got[2]

	if shell.Kind != "shell" || shell.TaskID != "bwsmza86c" || shell.Description != "Gate all five modules" {
		t.Errorf("shell = %+v", shell)
	}
	if shell.ToolUseID != "toolu_01NnQW8H1MwrsA4UymA3f89x" || shell.Status != "completed" {
		t.Errorf("shell ids/status = %q %q", shell.ToolUseID, shell.Status)
	}
	if shell.Exit == nil || *shell.Exit != 0 {
		t.Errorf("shell exit = %v, want 0", shell.Exit)
	}
	if shell.OutputFile != "<tmp>/tasks/bwsmza86c.output" {
		t.Errorf("shell output file = %q", shell.OutputFile)
	}
	if shell.At.IsZero() {
		t.Error("shell end time not parsed")
	}

	if agent.Kind != "subagent" || agent.Description != "Design critic plan-28b4a7f6" || agent.Exit != nil {
		t.Errorf("agent = %+v", agent)
	}
	if monitor.Kind != "monitor" || monitor.Description != "gate C finishing all five modules" {
		t.Errorf("monitor = %+v", monitor)
	}
}

func TestParseTaskNotifications_NonzeroExitAndQuotedCommand(t *testing.T) {
	line := `{"type":"queue-operation","operation":"enqueue","timestamp":"2026-10-27T16:31:00.000Z","content":"<task-notification>\n<task-id>bx1</task-id>\n<tool-use-id>toolu_X</tool-use-id>\n<output-file>/t/tasks/bx1.output</output-file>\n<status>failed</status>\n<summary>Background command \"go test ./... | grep \"FAIL\"\" failed with exit code 2</summary>\n</task-notification>"}`
	got := ParseTaskNotifications([]byte(line + "\n"))
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	n := got[0]
	if n.Exit == nil || *n.Exit != 2 {
		t.Errorf("exit = %v, want 2", n.Exit)
	}
	if n.Status != "failed" || n.Kind != "shell" {
		t.Errorf("status/kind = %q/%q", n.Status, n.Kind)
	}
	if n.Description != `go test ./... | grep "FAIL"` {
		t.Errorf("description = %q", n.Description)
	}
}

func TestParseTaskNotifications_IgnoresNonNotificationLines(t *testing.T) {
	lines := strings.Join([]string{
		`{"type":"assistant","message":{"content":"<task-notification><task-id>x</task-id><status>completed</status></task-notification>"}}`,
		`not json at all <task-notification>`,
		`{"type":"queue-operation","operation":"enqueue","content":"plain queued prompt"}`,
	}, "\n")
	if got := ParseTaskNotifications([]byte(lines)); len(got) != 0 {
		t.Fatalf("got %d notifications from non-notification lines: %+v", len(got), got)
	}
}

func TestReadTranscriptChunk_ResumesAndKeepsWholeLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	if err := os.WriteFile(path, []byte("aaaa\nbbbb\ncc"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf, next, err := ReadTranscriptChunk(path, 0, 1<<20)
	if err != nil || string(buf) != "aaaa\nbbbb\n" || next != 10 {
		t.Fatalf("first read = %q next=%d err=%v; want whole lines only", buf, next, err)
	}
	// The partial last line is read once it is complete, and only once.
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	f.WriteString("c\n")
	f.Close()
	buf, next, _ = ReadTranscriptChunk(path, next, 1<<20)
	if string(buf) != "ccc\n" || next != 14 {
		t.Fatalf("resume read = %q next=%d", buf, next)
	}
	if buf, _, _ = ReadTranscriptChunk(path, next, 1<<20); len(buf) != 0 {
		t.Fatalf("read past end = %q, want empty", buf)
	}
}

func TestReadTranscriptChunk_BoundedAndRecoversFromTruncation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.jsonl")
	os.WriteFile(path, []byte("1111\n2222\n3333\n"), 0o644)
	buf, next, _ := ReadTranscriptChunk(path, 0, 7)
	if string(buf) != "1111\n" || next != 5 {
		t.Fatalf("bounded read = %q next=%d", buf, next)
	}
	// A saved offset beyond the file (rewritten transcript) restarts at 0.
	buf, _, _ = ReadTranscriptChunk(path, 999, 1<<20)
	if string(buf) != "1111\n2222\n3333\n" {
		t.Fatalf("after truncation = %q", buf)
	}
	// A line longer than the bound is skipped rather than stalling forever.
	os.WriteFile(path, []byte(strings.Repeat("x", 50)+"\nok\n"), 0o644)
	_, next, _ = ReadTranscriptChunk(path, 0, 8)
	if next == 0 {
		t.Fatal("an over-long line must still advance the offset")
	}
	buf, _, _ = ReadTranscriptChunk(path, next, 1<<20)
	if !strings.HasSuffix(string(buf), "ok\n") || strings.Contains(string(buf), "xxx") {
		t.Fatalf("after over-long line = %q", buf)
	}
}

// LineEnd lets a caller stop after any notification and resume exactly at
// the next line.
func TestParseTaskNotifications_LineEndPointsPastTheLine(t *testing.T) {
	buf := readFixture(t)
	got := ParseTaskNotifications(buf)
	prev := 0
	for _, n := range got {
		if n.LineEnd <= prev || n.LineEnd > len(buf) || buf[n.LineEnd-1] != '\n' {
			t.Fatalf("LineEnd %d for %s is not a line end after %d", n.LineEnd, n.TaskID, prev)
		}
		if !strings.Contains(string(buf[prev:n.LineEnd]), "<task-id>"+n.TaskID+"</task-id>") {
			t.Errorf("line ending at %d does not hold task %s", n.LineEnd, n.TaskID)
		}
		prev = n.LineEnd
	}
	if rest := ParseTaskNotifications(buf[got[0].LineEnd:]); len(rest) != len(got)-1 {
		t.Errorf("resuming after the first notification found %d, want %d", len(rest), len(got)-1)
	}
}

// A bounded caller parses no further than the notifications it will use.
func TestParseTaskNotificationsN_StopsAtLimit(t *testing.T) {
	buf := readFixture(t)
	if got := ParseTaskNotificationsN(buf, 2); len(got) != 2 || got[1].Kind != "subagent" {
		t.Fatalf("limit 2 = %+v", got)
	}
	if got := ParseTaskNotificationsN(buf, 100); len(got) != 3 {
		t.Fatalf("limit above count = %d, want 3", len(got))
	}
	if got := ParseTaskNotificationsN(buf, 0); len(got) != 0 {
		t.Fatalf("limit 0 = %d, want 0", len(got))
	}
}
