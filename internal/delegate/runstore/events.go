package runstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Event kinds, as orch's hook map (docs/design/orch-inventory.md section 6).
const (
	KindSessionStart      = "session_start"
	KindPrompt            = "prompt"
	KindToolStart         = "tool_start"
	KindToolEnd           = "tool_end"
	KindQuestion          = "question"
	KindSubagentStart     = "subagent_start"
	KindSubagentStop      = "subagent_stop"
	KindNotification      = "notification"
	KindPermissionRequest = "permission_request"
	KindStop              = "stop"
	KindError             = "error"       // StopFailure: the turn ended on an API error
	KindSessionEnd        = "session_end" // SessionEnd: the summary is the end reason
)

// tornMark ends a torn last line before the next append. A torn line can be
// a whole JSON object that only lacks its newline; the mark makes it invalid
// JSON, so the reader keeps skipping it once it is terminated.
const tornMark = "#torn\n"

// eventVersion is the v field of every event line.
const eventVersion = 1

// tailBytes bounds how much of another writer's file is read for its last
// seq; a tail with no parseable line falls back to a full scan (orch P-7).
const tailBytes = 64 << 10

// Event is one line of events-<writer>.ndjson. Free text goes in Summary
// and must pass through Short first.
type Event struct {
	V         int    `json:"v"`
	Seq       int64  `json:"seq"`
	TS        string `json:"ts"`
	Run       string `json:"run"`
	Kind      string `json:"kind"`
	SID       string `json:"sid,omitempty"`
	Turn      int    `json:"turn,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Summary   string `json:"summary,omitempty"`
	// Writer is the file the event was read from; it is not stored.
	Writer string `json:"-"`
}

// EventWriter appends to one writer's events file. A writer name must be
// used by one process at a time: that is what keeps each file single-writer.
type EventWriter struct {
	run    *RunFolder
	writer string
	f      *os.File
	last   int64
	needNL bool
}

func eventsName(writer string) string { return "events-" + writer + ".ndjson" }

// OpenEvents opens (or creates) events-<writer>.ndjson in the run folder. It
// reads the writer's own last seq from the whole file, and marks a torn last
// line so the next append starts on a fresh line.
func (r *RunFolder) OpenEvents(writer string) (*EventWriter, error) {
	if !writerPattern.MatchString(writer) {
		return nil, fmt.Errorf("runstore: invalid events writer %q", writer)
	}
	path := filepath.Join(r.dir, eventsName(writer))
	if fi, err := os.Lstat(path); err == nil && !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("runstore: %s is not a plain file", path)
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_APPEND|os.O_CREATE, filePerm)
	if err != nil {
		return nil, fmt.Errorf("runstore: open events: %w", err)
	}
	data, err := io.ReadAll(f)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("runstore: read events: %w", err)
	}
	w := &EventWriter{run: r, writer: writer, f: f}
	w.needNL = len(data) > 0 && data[len(data)-1] != '\n'
	for _, ev := range parseLines(data) {
		w.last = max(w.last, ev.Seq)
	}
	return w, nil
}

// Append writes ev as one line with one write call. It fills v, run and ts,
// and sets seq above every seq this writer and the run's other writers have
// written so far, so the merged order follows what each writer had seen.
func (w *EventWriter) Append(ev Event) error {
	ev.V, ev.Run = eventVersion, w.run.id
	if ev.TS == "" {
		ev.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	ev.Seq = max(w.last, w.run.othersLastSeq(w.writer)) + 1
	line, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("runstore: encode event: %w", err)
	}
	line = append(line, '\n')
	if w.needNL {
		line = append([]byte(tornMark), line...)
	}
	if _, err := w.f.Write(line); err != nil {
		return fmt.Errorf("runstore: append event: %w", err)
	}
	w.last, w.needNL = ev.Seq, false
	return nil
}

// Close closes the writer's file.
func (w *EventWriter) Close() error { return w.f.Close() }

// ReadEvents merges every writer's file of the run in seq order (ties by
// writer name). Lines with no trailing newline and lines that do not parse
// are skipped, as are misnamed files and symlinks.
func (r *RunFolder) ReadEvents() ([]Event, error) {
	var all []Event
	for _, w := range r.writers() {
		data, err := os.ReadFile(filepath.Join(r.dir, eventsName(w)))
		if err != nil {
			continue // removed between listing and reading
		}
		for _, ev := range parseLines(data) {
			ev.Writer = w
			all = append(all, ev)
		}
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Seq != all[j].Seq {
			return all[i].Seq < all[j].Seq
		}
		return all[i].Writer < all[j].Writer
	})
	return all, nil
}

// writers lists the writer names of the run's plain events files.
func (r *RunFolder) writers() []string {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "events-") || !strings.HasSuffix(n, ".ndjson") {
			continue
		}
		w := strings.TrimSuffix(strings.TrimPrefix(n, "events-"), ".ndjson")
		if writerPattern.MatchString(w) && e.Type().IsRegular() {
			out = append(out, w)
		}
	}
	return out
}

// othersLastSeq is the highest seq in the other writers' files.
func (r *RunFolder) othersLastSeq(self string) int64 {
	var best int64
	for _, w := range r.writers() {
		if w != self {
			best = max(best, lastSeq(filepath.Join(r.dir, eventsName(w))))
		}
	}
	return best
}

// lastSeq reads the tail of path for its highest seq, scanning the whole
// file when the tail holds no complete parseable line. The tail starts on a
// line boundary (ReadTail), so a window that begins exactly at a line keeps
// that line (orch F-4).
func lastSeq(path string) int64 {
	buf, err := ReadTail(path, tailBytes)
	if err != nil {
		return 0
	}
	best := maxSeq(parseLines(buf))
	if fi, err := os.Stat(path); best == 0 && err == nil && fi.Size() > tailBytes {
		if all, err := os.ReadFile(path); err == nil {
			best = maxSeq(parseLines(all))
		}
	}
	return best
}

func maxSeq(evs []Event) int64 {
	var best int64
	for _, ev := range evs {
		best = max(best, ev.Seq)
	}
	return best
}

// parseLines parses the complete (newline-terminated) lines of data. A last
// line with no newline is a write in progress or a torn one: never parsed.
func parseLines(data []byte) []Event {
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		data = data[:i+1]
	} else {
		return nil
	}
	var out []Event
	for _, line := range bytes.Split(data, []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(line, &ev); err != nil || ev.Seq <= 0 || ev.Kind == "" {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// SubAgent is one sub-agent of a run's events.
type SubAgent struct {
	ID   string
	Type string
	SID  string
	Open bool // started, no matching stop yet
}

// SubAgents lists the run's sub-agents (decision D4): only subagent_start
// events with an agent id open one, and only a subagent_stop with the same
// agent id closes it. Other events that carry an agent id, such as plugin
// hook events or recap lines, never count, and a stop with no start is
// ignored.
func SubAgents(evs []Event) []SubAgent {
	var out []SubAgent
	index := map[string]int{}
	for _, ev := range evs {
		if ev.AgentID == "" {
			continue
		}
		switch ev.Kind {
		case KindSubagentStart:
			if _, seen := index[ev.AgentID]; !seen {
				index[ev.AgentID] = len(out)
				out = append(out, SubAgent{ID: ev.AgentID, Type: ev.AgentType, SID: ev.SID, Open: true})
			}
		case KindSubagentStop:
			if i, seen := index[ev.AgentID]; seen {
				out[i].Open = false
			}
		}
	}
	return out
}
