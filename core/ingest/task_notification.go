package ingest

import (
	"bytes"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// TaskNotification is the end of one background task as Claude Code reports
// it in the session transcript (feat-36658c22). Claude Code fires no hook when
// a background shell, subagent or monitor ends; instead it enqueues a
// <task-notification> for the model, which the transcript records as
//
//	{"type":"queue-operation","operation":"enqueue","timestamp":...,
//	 "content":"<task-notification>\n<task-id>…</task-id>\n<tool-use-id>…
//	 </tool-use-id>\n<output-file>…</output-file>\n<status>completed</status>
//	 \n<summary>Background command \"…\" completed (exit code 0)</summary>…"}
//
// The same text also reaches the transcript as a user message when the model
// reads it; only the enqueue line is parsed, so each end is seen once.
type TaskNotification struct {
	TaskID      string
	ToolUseID   string
	OutputFile  string
	Status      string // the harness's own word: completed, failed, killed, ...
	Summary     string
	Kind        string // shell | subagent | monitor | "" when unrecognised
	Description string // the quoted name in the summary
	Exit        *int   // from "(exit code N)"; nil when the summary has none
	At          time.Time
}

// taskNotificationMark is the substring every candidate line holds, so most
// transcript lines are skipped without being parsed.
var taskNotificationMark = []byte(`<task-notification>`)

var (
	summaryKindPattern = regexp.MustCompile(`^(Background command|Agent|Monitor) "(.*)" [a-z]`)
	exitCodePattern    = regexp.MustCompile(`exit code (-?\d+)\)?\s*$`)
)

// ParseTaskNotifications returns the task ends in buf, a run of complete
// transcript JSONL lines, in transcript order. Lines that are not an enqueued
// task-notification, or that carry no task id or status (remote-notification
// batches, interim monitor events), are skipped.
func ParseTaskNotifications(buf []byte) []TaskNotification {
	var out []TaskNotification
	for len(buf) > 0 {
		line := buf
		if nl := bytes.IndexByte(buf, '\n'); nl >= 0 {
			line, buf = buf[:nl], buf[nl+1:]
		} else {
			buf = nil
		}
		if !bytes.Contains(line, taskNotificationMark) {
			continue
		}
		if n, ok := parseNotificationLine(line); ok {
			out = append(out, n)
		}
	}
	return out
}

func parseNotificationLine(line []byte) (TaskNotification, bool) {
	if !gjson.ValidBytes(line) {
		return TaskNotification{}, false
	}
	r := gjson.ParseBytes(line)
	if r.Get("type").String() != "queue-operation" || r.Get("operation").String() != "enqueue" {
		return TaskNotification{}, false
	}
	content := r.Get("content").String()
	n := TaskNotification{
		TaskID:     xmlField(content, "task-id"),
		ToolUseID:  xmlField(content, "tool-use-id"),
		OutputFile: xmlField(content, "output-file"),
		Status:     xmlField(content, "status"),
		Summary:    xmlField(content, "summary"),
	}
	if n.TaskID == "" || n.Status == "" {
		return TaskNotification{}, false
	}
	n.At, _ = time.Parse(time.RFC3339Nano, r.Get("timestamp").String())
	n.Kind, n.Description = summaryKind(n.Summary)
	if m := exitCodePattern.FindStringSubmatch(n.Summary); m != nil {
		if code, err := strconv.Atoi(m[1]); err == nil {
			n.Exit = &code
		}
	}
	return n, true
}

// summaryKind maps the summary's lead words to a run kind and returns the
// quoted name. The name is greedy, so a command that itself holds quotes
// keeps them.
func summaryKind(summary string) (kind, description string) {
	m := summaryKindPattern.FindStringSubmatch(summary)
	if m == nil {
		return "", ""
	}
	switch m[1] {
	case "Background command":
		kind = "shell"
	case "Agent":
		kind = "subagent"
	case "Monitor":
		kind = "monitor"
	}
	return kind, m[2]
}

// xmlField returns the trimmed text between <name> and </name>, or "".
func xmlField(s, name string) string {
	open, closing := "<"+name+">", "</"+name+">"
	i := strings.Index(s, open)
	if i < 0 {
		return ""
	}
	rest := s[i+len(open):]
	j := strings.Index(rest, closing)
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

// ReadTranscriptChunk reads at most max bytes of the transcript from offset
// and returns only the complete lines in that window, plus the offset to
// resume from. It is the bounded tail reader the Stop and UserPromptSubmit
// hooks use: each pass reads what was appended since the last one.
//
//   - An offset past the end (the transcript was rewritten) restarts at 0.
//   - An offset in the middle of a line skips to the next line start.
//   - A window with no line end at all is skipped whole, so a line longer
//     than max can never stall the reader.
func ReadTranscriptChunk(path string, offset, max int64) ([]byte, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, offset, err
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	size := min(info.Size()-offset, max)
	if size <= 0 {
		return nil, offset, nil
	}
	buf := make([]byte, size)
	if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
		return nil, offset, err
	}
	start := 0
	if midLine(f, offset) {
		nl := bytes.IndexByte(buf, '\n')
		if nl < 0 {
			return nil, offset + size, nil
		}
		start = nl + 1
	}
	end := bytes.LastIndexByte(buf, '\n') + 1
	if end <= start {
		if size == max {
			return nil, offset + size, nil
		}
		return nil, offset + int64(start), nil
	}
	return buf[start:end], offset + int64(end), nil
}

// midLine reports whether offset falls inside a line: the byte before it is
// not a newline.
func midLine(f *os.File, offset int64) bool {
	if offset == 0 {
		return false
	}
	var b [1]byte
	if _, err := f.ReadAt(b[:], offset-1); err != nil {
		return false
	}
	return b[0] != '\n'
}
