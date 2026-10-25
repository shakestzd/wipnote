package hooks

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/fsprobe"
)

// SessionEndNoteFile is the session-scoped, append-only file SessionEnd writes
// first. It lives under .wipnote/sessions/<id>/, which .wipnote/.gitignore
// excludes, so it is runtime state and never committed.
const SessionEndNoteFile = "session-end.ndjson"

// sessionEndDoneFile marks a note whose deferred work the catch-up finished.
const sessionEndDoneFile = "session-end.done"

// SessionEndNote is the one durable fact a cancelled SessionEnd hook must leave
// behind: this session ended, when, why, and how its claims should be closed.
// Catch-up reads it and finishes whatever the hook did not reach.
type SessionEndNote struct {
	SessionID      string `json:"session_id"`
	EndedAt        string `json:"ended_at"`
	Reason         string `json:"reason,omitempty"`
	TranscriptPath string `json:"transcript_path,omitempty"`
	// Outcome is how the session's open claim episodes end. SessionEnd writes
	// "abandoned"; the reaper's "expired" never comes from a note.
	Outcome string `json:"outcome,omitempty"`
}

// EndTime parses EndedAt. The zero time means the note is unusable.
func (n SessionEndNote) EndTime() time.Time {
	t, err := time.Parse(time.RFC3339Nano, n.EndedAt)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// ClaimOutcome returns the episode outcome the note records, defaulting to the
// outcome a live SessionEnd uses.
func (n SessionEndNote) ClaimOutcome() claimledger.Outcome {
	if n.Outcome == "" {
		return claimledger.OutcomeAbandoned
	}
	return claimledger.Outcome(n.Outcome)
}

// sessionDirFor returns .wipnote/sessions/<id>, or "" when id could escape the
// sessions directory.
func sessionDirFor(projectDir, sessionID string) string {
	if projectDir == "" || sessionID == "" || sessionID == "." || sessionID == ".." ||
		filepath.Base(sessionID) != sessionID {
		return ""
	}
	return filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
}

var (
	noteMu      sync.Mutex
	noteWritten = map[string]bool{}
)

// RecordSessionEndNote appends the end note for event's session and fsyncs it.
//
// It is the FIRST thing SessionEnd does. It spawns no process, scans no ledger
// shard and takes no lock: one directory create, one O_APPEND write of a single
// line, one fsync. The line goes out in one write(2), so a hook killed at any
// point leaves either no line or a complete one; a torn tail (power loss) is
// skipped by ReadSessionEndNote. Returns false when nothing was written. A
// second call for the same session in this process is a no-op, because the
// cobra wrapper writes the note early and the handler asks again.
func RecordSessionEndNote(event *CloudEvent, projectDir string) bool {
	sessionID := EnvSessionID(event.SessionID)
	dir := sessionDirFor(projectDir, sessionID)
	if dir == "" {
		return false
	}
	noteMu.Lock()
	defer noteMu.Unlock()
	key := dir
	if noteWritten[key] {
		return true
	}
	line, err := json.Marshal(SessionEndNote{
		SessionID:      sessionID,
		EndedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		Reason:         event.Reason,
		TranscriptPath: event.TranscriptPath,
		Outcome:        string(claimledger.OutcomeAbandoned),
	})
	if err != nil {
		return false
	}
	fsprobe.Touch(fsprobe.OpWrite, filepath.Join(dir, SessionEndNoteFile))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		debugLog(projectDir, "[session-end] note mkdir: %v", err)
		return false
	}
	f, err := os.OpenFile(filepath.Join(dir, SessionEndNoteFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		debugLog(projectDir, "[session-end] note open: %v", err)
		return false
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return false
	}
	if err := f.Sync(); err != nil {
		return false
	}
	noteWritten[key] = true
	return true
}

// ReadSessionEndNote returns the latest complete, valid note for sessionID. A
// missing file, a torn tail and a line for another session all read as "no
// note" rather than an error: the reaper is the backstop for those.
func ReadSessionEndNote(projectDir, sessionID string) (SessionEndNote, bool) {
	dir := sessionDirFor(projectDir, sessionID)
	if dir == "" {
		return SessionEndNote{}, false
	}
	f, err := os.Open(filepath.Join(dir, SessionEndNoteFile))
	if err != nil {
		return SessionEndNote{}, false
	}
	defer f.Close()
	var best SessionEndNote
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var n SessionEndNote
		if json.Unmarshal(sc.Bytes(), &n) != nil || n.SessionID != sessionID || n.EndTime().IsZero() {
			continue
		}
		if !found || n.EndTime().After(best.EndTime()) {
			best, found = n, true // a resumed session that ends again supersedes its earlier end
		}
	}
	return best, found
}
