package hooks

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/ingest"
	"github.com/shakestzd/wipnote/core/runledger"
)

// The end of a Claude Code background task is a <task-notification> in the
// session transcript; no hook fires for it. Stop and UserPromptSubmit read the
// transcript from the offset the previous pass saved, at most
// runTranscriptChunkBytes per pass, and append one end event per
// notification. The offset lives in the gitignored session folder.

// runTranscriptChunkBytes bounds one pass. A long backlog (a resumed session
// with no saved offset) is caught up over several turns.
var runTranscriptChunkBytes int64 = 4 << 20

const runOffsetFile = ".runs-transcript-offset"

// syncTranscriptRunsFor runs the pass for a Claude hook event.
func syncTranscriptRunsFor(event *CloudEvent, projectDir, sessionID string) {
	if event == nil || event.Harness != HarnessClaude || event.TranscriptPath == "" {
		return
	}
	SyncTranscriptRuns(projectDir, sessionID, event.TranscriptPath)
}

// SyncTranscriptRuns appends an end event for every task-notification written
// to the transcript since the last pass and returns how many it appended. It
// is also the on-demand reader for commands that want ends up to date.
func SyncTranscriptRuns(projectDir, sessionID, transcriptPath string) int {
	if projectDir == "" || sessionID == "" || transcriptPath == "" {
		return 0
	}
	offsetPath := filepath.Join(projectDir, ".wipnote", "sessions", sessionID, runOffsetFile)
	offset := readRunOffset(offsetPath)
	buf, next, err := ingest.ReadTranscriptChunk(transcriptPath, offset, runTranscriptChunkBytes)
	if err != nil {
		return 0
	}
	n := 0
	for _, note := range ingest.ParseTaskNotifications(buf) {
		appendRunEvent(projectDir, notificationEnd(projectDir, sessionID, note))
		n++
	}
	if next != offset {
		writeRunOffset(offsetPath, next)
	}
	return n
}

// notificationEnd turns one notification into an end event. The output file
// is summarised for shells and monitors; a subagent's output file is its
// transcript, which has no meaningful last line.
func notificationEnd(projectDir, sessionID string, note ingest.TaskNotification) runledger.Event {
	at := note.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	e := runledger.Event{
		RunID: runIDFor(sessionID, note.ToolUseID, note.TaskID), Phase: runledger.PhaseEnd, At: at,
		TaskID: note.TaskID, ToolUseID: note.ToolUseID, Kind: note.Kind,
		Description: runledger.Truncate(runledger.Clean(note.Description, projectDir), 200),
		Status:      runledger.MapStatus(note.Status, note.Exit), Exit: note.Exit,
		LogPath: runledger.StoredPath(note.OutputFile, projectDir),
		Session: sessionID, Harness: HarnessClaude.String(),
	}
	if note.Kind != runledger.KindSubagent && note.OutputFile != "" {
		last, first := runledger.SummarizeFile(note.OutputFile)
		e.LastLine, e.FirstError = runledger.Clean(last, projectDir), runledger.Clean(first, projectDir)
	}
	return e
}

func readRunOffset(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func writeRunOffset(path string, offset int64) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strconv.FormatInt(offset, 10)+"\n"), 0o644)
}
