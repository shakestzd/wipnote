package sessionledger

import (
	"bytes"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/shakestzd/wipnote/core/fsprobe"
	"github.com/shakestzd/wipnote/core/htmlledger"
)

// ledgerHeader and ledgerFooter bracket the row lines. They are byte-stable
// constants because the append path finds the write offset by matching the
// footer against the file's trailing bytes — any drift here must be a
// deliberate format change, and would be caught by the round-trip tests.
const ledgerHeader = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Sessions</title></head><body>
<main>
<table data-session-ledger="true">
<thead><tr><th>Session</th><th>Harness</th><th>Project</th><th>Start</th><th>End</th><th>End source</th><th>Archive</th><th>Events</th></tr></thead>
<tbody>
`

const ledgerFooter = `</tbody>
</table>
</main>
</body></html>
`

// rowSelector is the parse contract: only <tr> elements inside the
// session-ledger table that carry a session id are records. A torn tail that
// goquery renders as some other element is therefore ignored rather than
// mis-parsed.
const rowSelector = `table[data-session-ledger] tbody tr[data-session-id]`

// recordSelector picks session rows and run-event lines (runs.go) in one pass,
// in document order, which is the order run events fold in.
const recordSelector = rowSelector + ", " + runSelector

// ledgerFormat is this ledger's document shape for the shared HTML codec.
var ledgerFormat = htmlledger.Format{
	Name:        "sessionledger",
	Header:      ledgerHeader,
	Footer:      ledgerFooter,
	RowSelector: recordSelector,
	TempPattern: ".sessions-ledger-*.tmp",
}

// marshalRow renders one session as exactly ONE line terminated by '\n'.
//
// The single-line invariant is what makes the torn-write repair sound: the
// repair truncates back to the last newline, which is always a row boundary. It
// is enforced by sanitizing every field, not merely assumed — a harness name or
// archive path carrying a newline would otherwise let one corrupt write swallow
// the rows after it.
func marshalRow(r Record) []byte {
	var buf bytes.Buffer
	buf.WriteString("<tr")
	htmlledger.WriteAttr(&buf, "id", r.SessionID)
	htmlledger.WriteAttr(&buf, "data-session-id", r.SessionID)
	htmlledger.WriteAttr(&buf, "data-harness", r.Harness)
	htmlledger.WriteAttr(&buf, "data-project", r.ProjectDir)
	htmlledger.WriteAttr(&buf, "data-start", FormatTime(r.StartedAt))
	endStr := ""
	endSrc := ""
	if !r.IsOpen() {
		endStr = FormatTime(r.EndedAt)
		endSrc = string(r.EndSource)
	}
	htmlledger.WriteAttr(&buf, "data-end", endStr)
	htmlledger.WriteAttr(&buf, "data-end-source", endSrc)
	htmlledger.WriteAttr(&buf, "data-archive", r.ArchivePath)
	eventsStr := ""
	if r.Events > 0 {
		eventsStr = strconv.Itoa(r.Events)
	}
	htmlledger.WriteAttr(&buf, "data-events", eventsStr)
	buf.WriteString(">")

	htmlledger.WriteCell(&buf, "session-id", r.SessionID)
	htmlledger.WriteCell(&buf, "harness", r.Harness)
	htmlledger.WriteCell(&buf, "project", r.ProjectDir)
	htmlledger.WriteCell(&buf, "start", FormatTime(r.StartedAt))
	htmlledger.WriteCell(&buf, "end", endStr)
	htmlledger.WriteCell(&buf, "end-source", endSrc)
	htmlledger.WriteCell(&buf, "archive", r.ArchivePath)
	htmlledger.WriteCell(&buf, "events", eventsStr)

	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// ReadFile parses the ledger into records, ordered by start time. A missing
// file reports os.IsNotExist so callers can treat it as empty.
//
// Rows are read leniently: a row that fails validation is skipped rather than
// failing the whole file, so one bad row can never hide every other session.
// This matches the claim ledger's and the archive ledger's behaviour.
func ReadFile(path string) ([]Record, error) {
	fsprobe.Touch(fsprobe.OpRead, path)
	var c rowCollector
	if err := ledgerFormat.ReadRows(path, c.add); err != nil {
		return nil, err
	}
	return c.result(), nil
}

func parseRow(row *goquery.Selection) (Record, bool) {
	r := Record{
		SessionID:   strings.TrimSpace(htmlledger.Attr(row, "data-session-id")),
		Harness:     strings.TrimSpace(htmlledger.Attr(row, "data-harness")),
		ProjectDir:  strings.TrimSpace(htmlledger.Attr(row, "data-project")),
		ArchivePath: strings.TrimSpace(htmlledger.Attr(row, "data-archive")),
	}
	started, err := ParseTime(htmlledger.Attr(row, "data-start"))
	if err != nil {
		return Record{}, false
	}
	r.StartedAt = started
	ended, err := ParseTime(htmlledger.Attr(row, "data-end"))
	if err != nil {
		return Record{}, false
	}
	r.EndedAt = ended
	// Rows written before provenance existed have no data-end-source and read as
	// EndSourceUnknown, which ranks lowest — so repair re-derives exactly those.
	if !r.IsOpen() {
		r.EndSource = EndSource(strings.TrimSpace(htmlledger.Attr(row, "data-end-source")))
	}
	if n, convErr := strconv.Atoi(strings.TrimSpace(htmlledger.Attr(row, "data-events"))); convErr == nil && n > 0 {
		r.Events = n
	}
	if r.Validate() != nil {
		return Record{}, false
	}
	return r, true
}

// sortRecords orders by start, then by id so the order is total and stable
// across rewrites (two sessions can share a start timestamp).
func sortRecords(recs []Record) {
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].StartedAt.Equal(recs[j].StartedAt) {
			return recs[i].StartedAt.Before(recs[j].StartedAt)
		}
		return recs[i].SessionID < recs[j].SessionID
	})
}

// appendRowLocked appends one row at the tail of path in CONSTANT time: it
// overwrites the trailing footer with row+footer instead of parsing and
// reserializing every existing row.
//
// The caller must already hold the write guard for path.
//
// TORN-WRITE GUARD (the reason this is not a naive seek-to-end append): a crash
// mid-append leaves a file whose tail is neither a complete row nor the footer.
// Appending after that tail would merge the new row INTO the corrupt fragment —
// goquery parses `<tr data-sessi` + `<tr …>` as one garbage element and
// SWALLOWS the new row. So before writing we locate a known-good boundary: the
// footer if the file ends with it, otherwise the last newline, which is by
// construction the end of the last complete row.
func appendRowLocked(path string, r Record) error {
	return ledgerFormat.AppendRow(path, marshalRow(r))
}

// writeAllLocked rewrites path with exactly the given records, atomically via
// temp-then-rename. This is the UPDATE path: recording an end or an archive
// path on an existing row is inherently a read-modify-write, so it pays the
// full serialize cost. Session starts — the frequent operation — never come
// through here.
//
// The caller must already hold the write guard for path.
func writeAllLocked(path string, records []Record) error {
	fsprobe.Touch(fsprobe.OpWrite, path)
	sorted := make([]Record, len(records))
	copy(sorted, records)
	sortRecords(sorted)
	rows := make([][]byte, 0, len(sorted))
	for _, r := range sorted {
		rows = append(rows, marshalRecord(r))
	}
	return ledgerFormat.WriteAll(path, rows)
}
