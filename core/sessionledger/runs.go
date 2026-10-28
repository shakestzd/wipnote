package sessionledger

import (
	"bytes"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/shakestzd/wipnote/core/htmlledger"
)

// # Runs (bug-2a96e9cb)
//
// `claude --resume <id>` and a Codex resume reuse the session id, so a session
// can run, end, and run again. The session keeps ONE row; each process lifetime
// is a run with its own start and end.
//
// Storage stays the ledger's own shape. The session row (<tr data-session-id>)
// is run 1, byte-for-byte as before, so a binary that predates runs reads a
// resumed session exactly as it did. Every later run is recorded as append-only
// run-event lines after it:
//
//	<tr data-run-of="<id>" data-run-event="start" data-start="<ts>">…</tr>
//	<tr data-run-of="<id>" data-run-event="end" data-end="<ts>" data-end-source="live-close">…</tr>
//
// They carry no data-session-id, so an older reader's row selector never sees
// them. Events fold in document order: a start opens a run only while the
// latest run is closed (anything else is a replay), and an end closes the
// latest resumed run only while it is open (the first recorded end is the true
// one, as for run 1). A ledger with no run events reads as one implicit run.

// runSelector picks the run-event lines.
const runSelector = `table[data-session-ledger] tbody tr[data-run-of]`

const (
	runEventStart = "start"
	runEventEnd   = "end"
)

// Run is one process lifetime of a session. EndedAt is zero while it runs.
type Run struct {
	StartedAt time.Time
	EndedAt   time.Time
	EndSource EndSource
}

// IsOpen reports whether the run has no recorded end.
func (r Run) IsOpen() bool { return r.EndedAt.IsZero() }

// RunList returns the session's runs, first to latest. A session that never
// resumed is one run, StartedAt..EndedAt. The slice is a copy.
func (r Record) RunList() []Run {
	if len(r.Runs) > 0 {
		return append([]Run(nil), r.Runs...)
	}
	return []Run{{StartedAt: r.StartedAt, EndedAt: r.EndedAt, EndSource: r.EndSource}}
}

// LatestRun returns the session's latest run.
func (r Record) LatestRun() Run {
	runs := r.RunList()
	return runs[len(runs)-1]
}

// resume starts a new run on a closed record and returns its start. A start
// before the previous end (clock skew) is moved up to that end, so runs never
// overlap.
func (r *Record) resume(at time.Time) time.Time {
	if at.Before(r.EndedAt) {
		at = r.EndedAt
	}
	r.Runs = append(r.RunList(), Run{StartedAt: at})
	r.EndedAt, r.EndSource = time.Time{}, EndSourceUnknown
	return at
}

// endLatest records end on the latest run. It copies Runs first, so a record
// copied before the call (Enrich's before/after comparison) is not changed.
func (r *Record) endLatest(end time.Time, src EndSource) {
	r.EndedAt, r.EndSource = end, src
	if n := len(r.Runs); n > 0 {
		r.Runs = append([]Run(nil), r.Runs...)
		r.Runs[n-1].EndedAt, r.Runs[n-1].EndSource = end, src
	}
}

// resumeLocked appends a run-start event for a closed record and applies it
// in memory, so a harness correction that follows rewrites the run too.
// Caller holds the lock.
func (s *Store) resumeLocked(rec *Record, at time.Time) (bool, error) {
	at = rec.resume(at)
	line := marshalRunEvent(rec.SessionID, runEvent{kind: runEventStart, at: at})
	if err := ledgerFormat.AppendRow(s.path, line); err != nil {
		return false, err
	}
	s.notify("session resume")
	return true, nil
}

// resumed reports whether the record has runs after the first.
func (r Record) resumed() bool { return len(r.Runs) > 1 }

// runEvent is one run-event line.
type runEvent struct {
	kind string
	at   time.Time
	src  EndSource
}

// apply folds one event into the record (see the package rule above).
func (r *Record) apply(e runEvent) {
	switch {
	case e.kind == runEventStart && !r.IsOpen():
		r.resume(e.at)
	case e.kind == runEventEnd && r.IsOpen() && r.resumed():
		end := e.at
		if start := r.LatestRun().StartedAt; end.Before(start) {
			end = start
		}
		r.endLatest(end, e.src)
	}
}

// marshalRunEvent renders one run event as ONE line, laid out in the ledger's
// columns so the table still reads in a browser.
func marshalRunEvent(sessionID string, e runEvent) []byte {
	var buf bytes.Buffer
	startStr, endStr, srcStr := "", "", ""
	if e.kind == runEventStart {
		startStr = FormatTime(e.at)
	} else {
		endStr, srcStr = FormatTime(e.at), string(e.src)
	}
	buf.WriteString("<tr")
	htmlledger.WriteAttr(&buf, "data-run-of", sessionID)
	htmlledger.WriteAttr(&buf, "data-run-event", e.kind)
	htmlledger.WriteAttr(&buf, "data-start", startStr)
	htmlledger.WriteAttr(&buf, "data-end", endStr)
	htmlledger.WriteAttr(&buf, "data-end-source", srcStr)
	buf.WriteString(">")
	for _, c := range [][2]string{{"session-id", sessionID}, {"harness", ""}, {"project", ""},
		{"start", startStr}, {"end", endStr}, {"end-source", srcStr}, {"archive", ""}, {"events", ""}} {
		htmlledger.WriteCell(&buf, c[0], c[1])
	}
	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// parseRunEvent reads one run-event line; ok is false for a malformed one.
func parseRunEvent(row *goquery.Selection) (string, runEvent, bool) {
	id := strings.TrimSpace(htmlledger.Attr(row, "data-run-of"))
	e := runEvent{kind: strings.TrimSpace(htmlledger.Attr(row, "data-run-event"))}
	attr := "data-start"
	if e.kind == runEventEnd {
		attr = "data-end"
		e.src = EndSource(strings.TrimSpace(htmlledger.Attr(row, "data-end-source")))
	} else if e.kind != runEventStart {
		return "", runEvent{}, false
	}
	at, err := ParseTime(htmlledger.Attr(row, attr))
	if err != nil || at.IsZero() || id == "" {
		return "", runEvent{}, false
	}
	e.at = at
	return id, e, true
}

// marshalRecord renders a session: its row, which carries run 1, then one
// event line per later run start and end.
func marshalRecord(r Record) []byte {
	if !r.resumed() {
		return marshalRow(r)
	}
	first := r.Runs[0]
	base := r
	base.StartedAt, base.EndedAt, base.EndSource = first.StartedAt, first.EndedAt, first.EndSource
	out := marshalRow(base)
	for _, run := range r.Runs[1:] {
		out = append(out, marshalRunEvent(r.SessionID, runEvent{kind: runEventStart, at: run.StartedAt})...)
		if !run.IsOpen() {
			ev := runEvent{kind: runEventEnd, at: run.EndedAt, src: run.EndSource}
			out = append(out, marshalRunEvent(r.SessionID, ev)...)
		}
	}
	return out
}

// rowCollector gathers session rows and run events in document order and
// folds the events into their sessions.
type rowCollector struct {
	records []Record
	ids     []string
	events  []runEvent
}

func (c *rowCollector) add(row *goquery.Selection) {
	if _, isEvent := row.Attr("data-run-of"); isEvent {
		if id, e, ok := parseRunEvent(row); ok {
			c.ids, c.events = append(c.ids, id), append(c.events, e)
		}
		return
	}
	if r, ok := parseRow(row); ok {
		c.records = append(c.records, r)
	}
}

// result applies every event to the first row for its session, in document
// order, and returns the records sorted. An event whose session has no valid
// row is dropped: it cannot make a session exist.
func (c *rowCollector) result() []Record {
	index := make(map[string]int, len(c.records))
	for i := len(c.records) - 1; i >= 0; i-- {
		index[c.records[i].SessionID] = i
	}
	for i, e := range c.events {
		if idx, ok := index[c.ids[i]]; ok {
			c.records[idx].apply(e)
		}
	}
	sortRecords(c.records)
	return c.records
}
