package hooks

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
)

// Stop and SessionEnd match unfinished tool-log starts to the transcript, and
// SubagentStop to the subagent's own transcript (feat-ddeaa0d1). A call denied in the permission dialog, by a deny rule, or
// left unanswered fires no PermissionDenied, PostToolUse or
// PostToolUseFailure hook, but the transcript records its tool_result: a user
// entry whose content holds {"type":"tool_result","tool_use_id":...,
// "is_error":true}. Real transcripts also put "toolDenialKind" (seen:
// "cancelled") on the entry for an approval that never ran. Any error result
// for an unfinished start becomes a failed line; a non-error result writes
// nothing, since its own async PostToolUse record may still be on the way.
//
// Bounds: nothing is read when the log has no unfinished start, which is
// most turns; otherwise only the last transcriptTailBytes of the transcript.
// Stop runs at every turn end, so the tail since the last turn is what holds a
// new denial. The tool log is taken with the sweeper's non-blocking lock, so a
// concurrent session-start sweep and this pass never both record a call.

// transcriptTailBytes bounds how much of the transcript one pass reads.
var transcriptTailBytes int64 = 4 << 20

// recordTranscriptFailuresFor runs the transcript pass for a Stop or
// SessionEnd event over the main session's transcript.
func recordTranscriptFailuresFor(event *CloudEvent, projectDir, sessionID string) {
	if event == nil {
		return
	}
	recordClaudeTranscriptFailures(event, projectDir, sessionID, event.TranscriptPath)
}

// recordClaudeTranscriptFailures runs the pass over one transcript. Only
// Claude Code transcripts have this shape. SubagentStop passes the subagent's
// own transcript (agent_transcript_path): a subagent's hooks carry the
// parent's session_id, so its start lines are in the same tool log.
func recordClaudeTranscriptFailures(event *CloudEvent, projectDir, sessionID, transcriptPath string) {
	if transcriptPath == "" || event.Harness != HarnessClaude {
		return
	}
	RecordTranscriptToolFailures(projectDir, sessionID, transcriptPath)
}

// RecordTranscriptToolFailures writes a failed line for every unfinished start
// in the session's tool log whose tool_result in the transcript tail is an
// error. It returns how many it recorded.
func RecordTranscriptToolFailures(projectDir, sessionID, transcriptPath string) int {
	if projectDir == "" || sessionID == "" || transcriptPath == "" {
		return 0
	}
	data, unlock, ok := lockToolLogForSweep(canonicalToolLogPath(projectDir, sessionID))
	if !ok {
		return 0
	}
	defer unlock()
	starts, done := parseToolLogPairs(data)
	open := map[string]bool{}
	for _, s := range starts {
		if !done[s.id] {
			open[s.id] = true
		}
	}
	if len(open) == 0 {
		return 0
	}
	reasons := transcriptErrorResults(transcriptPath, sessionID, open)
	recorded := 0
	for _, s := range starts {
		if reason, hit := reasons[s.id]; hit && open[s.id] {
			delete(open, s.id)
			if recordFailedToolCall(projectDir, sessionID, s, reason) {
				recorded++
			}
		}
	}
	return recorded
}

// transcriptEntry is the part of a transcript line the pass reads.
type transcriptEntry struct {
	ToolDenialKind string `json:"toolDenialKind"`
	Message        struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// transcriptResultBlock is one content block of a user entry.
type transcriptResultBlock struct {
	Type      string `json:"type"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

// transcriptErrorResults maps the tool-log id of each error tool_result in
// the transcript tail that belongs to an open call to its failure reason.
func transcriptErrorResults(path, sessionID string, open map[string]bool) map[string]string {
	out := map[string]string{}
	for _, line := range transcriptErrorLines(readTranscriptTail(path)) {
		var entry transcriptEntry
		var blocks []transcriptResultBlock
		if json.Unmarshal(line, &entry) != nil || json.Unmarshal(entry.Message.Content, &blocks) != nil {
			continue
		}
		reason := "tool-error"
		if entry.ToolDenialKind != "" {
			reason = "denied-" + strings.ToLower(entry.ToolDenialKind)
		}
		for _, b := range blocks {
			id := toolCallEventID(sessionID, b.ToolUseID)
			if b.Type == "tool_result" && b.IsError && open[id] {
				out[id] = reason
			}
		}
	}
	return out
}

// transcriptErrorMark is how Claude Code's compact JSONL spells an error
// tool_result; only lines holding it are parsed.
var transcriptErrorMark = []byte(`"is_error":true`)

// transcriptErrorLines returns each complete line of buf that holds
// transcriptErrorMark, without splitting the whole buffer into lines. A
// partial first line (the read began mid-line) is dropped by the caller's
// offset handling; a line with no trailing newline is still complete.
func transcriptErrorLines(buf []byte) [][]byte {
	var out [][]byte
	for {
		i := bytes.Index(buf, transcriptErrorMark)
		if i < 0 {
			return out
		}
		start := bytes.LastIndexByte(buf[:i], '\n') + 1
		end := bytes.IndexByte(buf[i:], '\n')
		if end < 0 {
			end = len(buf)
		} else {
			end += i
		}
		out = append(out, buf[start:end])
		buf = buf[end:]
	}
}

// readTranscriptTail returns the last transcriptTailBytes of the transcript,
// starting at a line boundary; nil when it cannot be read.
func readTranscriptTail(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil
	}
	offset := info.Size() - transcriptTailBytes
	if offset < 0 {
		offset = 0
	}
	buf := make([]byte, info.Size()-offset) // one allocation, no ReadAll growth
	if _, err := f.ReadAt(buf, offset); err != nil && err != io.EOF {
		return nil
	}
	if offset == 0 {
		return buf
	}
	if nl := bytes.IndexByte(buf, '\n'); nl >= 0 {
		return buf[nl+1:] // the read began mid-line
	}
	return nil
}
