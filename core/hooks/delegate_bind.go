package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Delegate worker events (plan-28b4a7f6 slice 10). A worker started by
// `wipnote delegate spawn` carries WIPNOTE_DELEGATE_RUN; its hooks append one
// line per event to its own events-<session>.ndjson in the run folder
// (.wipnote/delegate/runs/<run>/), the line format internal/delegate/runstore
// reads (v1). The SessionStart line is the bind: the next delegate list, wait
// or stop moves the session into the ledger's worker_sessions.
//
// It is on every hook's hot path, so it does one directory read, a bounded
// tail read per events file and one append, takes no lock, and fails open:
// any problem writes nothing and returns.

// DelegateRunEnv names the delegate run a worker session belongs to.
const DelegateRunEnv = "WIPNOTE_DELEGATE_RUN"

// DelegateEvent is one event a worker hook records. Kind is a runstore event
// kind (session_start, prompt, stop, subagent_start, subagent_stop).
type DelegateEvent struct {
	Kind      string
	SID       string
	AgentID   string
	AgentType string
}

var (
	delegateRunRe    = regexp.MustCompile(`^dlg-[0-9a-f]{12,64}$`)
	delegateWriterRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// delegateTail bounds the bytes read from each events file for its last seq.
const delegateTail = 4 << 10

type delegateLine struct {
	V         int    `json:"v"`
	Seq       int64  `json:"seq"`
	TS        string `json:"ts"`
	Run       string `json:"run"`
	Kind      string `json:"kind"`
	SID       string `json:"sid,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
}

// RecordDelegateEvent appends ev to the worker's events file when this
// process runs inside a delegate worker. It never blocks and never fails.
func RecordDelegateEvent(projectDir string, ev DelegateEvent) {
	run := strings.TrimSpace(os.Getenv(DelegateRunEnv))
	if run == "" || projectDir == "" || !delegateRunRe.MatchString(run) || !delegateWriterRe.MatchString(ev.SID) {
		return
	}
	dir := filepath.Join(projectDir, ".wipnote", "delegate", "runs", run)
	if fi, err := os.Lstat(dir); err != nil || !fi.IsDir() {
		return // no run folder, or a symlink: not ours
	}
	path := filepath.Join(dir, "events-"+ev.SID+".ndjson")
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return
	}
	line := delegateLine{V: 1, TS: time.Now().UTC().Format(time.RFC3339Nano), Run: run, Kind: ev.Kind,
		SID: ev.SID, AgentID: ev.AgentID, AgentType: ev.AgentType}
	seq, torn := delegateNextSeq(dir, path)
	line.Seq = seq
	data, err := json.Marshal(line)
	if err != nil {
		return
	}
	data = append(data, '\n')
	if torn {
		data = append([]byte("#torn\n"), data...)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	_ = f.Close()
}

// delegateNextSeq is one above the highest seq at the tail of every events
// file in the run folder, so the merged order follows what this writer had
// seen. torn reports that own (the writer's file) ends inside a line.
func delegateNextSeq(dir, own string) (seq int64, torn bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 1, false
	}
	var top int64
	for _, e := range entries {
		n := e.Name()
		if !e.Type().IsRegular() || !strings.HasPrefix(n, "events-") || !strings.HasSuffix(n, ".ndjson") {
			continue
		}
		path := filepath.Join(dir, n)
		tail := readDelegateTail(path)
		if path == own && len(tail) > 0 && tail[len(tail)-1] != '\n' {
			torn = true
		}
		for _, l := range strings.Split(string(tail), "\n") {
			var dl delegateLine
			if json.Unmarshal([]byte(l), &dl) == nil && dl.Seq > top {
				top = dl.Seq
			}
		}
	}
	return top + 1, torn
}

func readDelegateTail(path string) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(fi.Size()-delegateTail, 0)
	buf := make([]byte, fi.Size()-off)
	n, _ := f.ReadAt(buf, off)
	return buf[:n]
}

// recordDelegateHook records a hook event for a delegate worker session.
// A subagent's own SessionStart never binds a session.
func recordDelegateHook(event *CloudEvent, projectDir, sessionID, kind string) {
	if event == nil || os.Getenv(DelegateRunEnv) == "" {
		return
	}
	if kind == "session_start" && isSubagentEvent(event) {
		return
	}
	RecordDelegateEvent(projectDir, DelegateEvent{Kind: kind, SID: sessionID, AgentID: event.AgentID, AgentType: event.AgentType})
}
