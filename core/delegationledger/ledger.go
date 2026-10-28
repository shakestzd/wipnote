package delegationledger

import (
	"bytes"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"

	"github.com/shakestzd/wipnote/core/htmlledger"
)

// ledgerHeader and ledgerFooter bracket the row lines. They are byte-stable:
// the append path finds its offset by matching the footer against the file's
// trailing bytes.
const ledgerHeader = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Delegation Runs</title></head><body>
<main>
<table data-delegation-ledger="true">
<thead><tr><th>Run</th><th>Mode</th><th>Harness</th><th>Name</th><th>Label</th><th>Work Item</th><th>Link</th><th>Caller Session</th><th>Worker Sessions</th><th>Host</th><th>Started</th><th>Ended</th><th>Exit</th><th>End Reason</th><th>Input</th><th>Cached</th><th>Output</th><th>Reasoning</th><th>Resumed From</th></tr></thead>
<tbody>
`

const ledgerFooter = `</tbody>
</table>
</main>
</body></html>
`

// rowSelector: only rows of this table that carry a run id are records, so a
// torn tail goquery renders as some other element is ignored.
const rowSelector = `table[data-delegation-ledger] tbody tr[data-run-id]`

var ledgerFormat = htmlledger.Format{
	Name:        "delegationledger",
	Header:      ledgerHeader,
	Footer:      ledgerFooter,
	RowSelector: rowSelector,
	TempPattern: ".delegations-ledger-*.tmp",
}

// column is one ledger field: its attribute/cell name and its string form.
type column struct{ name, value string }

// columns renders r in column order. Attributes and cells use the same
// names, so the parse side reads attributes only.
func columns(r Record) []column {
	ended := ""
	if !r.Ended.IsZero() {
		ended = FormatTime(r.Ended)
	}
	exit := ""
	if r.Exit != nil {
		exit = strconv.Itoa(*r.Exit)
	}
	return []column{
		{"run-id", r.ID}, {"mode", r.Mode}, {"harness", r.Harness},
		{"name", r.Name}, {"label", r.Label}, {"work-item", r.WorkItem},
		{"link", r.Link}, {"caller-session", r.CallerSession},
		{"worker-sessions", strings.Join(r.WorkerSessions, " ")},
		{"host", r.Host}, {"started", FormatTime(r.Started)}, {"ended", ended},
		{"exit", exit}, {"end-reason", r.EndReason},
		{"usage-input", strconv.Itoa(r.UsageInput)},
		{"usage-cached", strconv.Itoa(r.UsageCached)},
		{"usage-output", strconv.Itoa(r.UsageOutput)},
		{"usage-reasoning", strconv.Itoa(r.UsageReasoning)},
		{"resumed-from", r.ResumedFrom},
	}
}

// marshalRow renders one run as exactly ONE line ending in '\n'. Every value
// is sanitized by htmlledger, which keeps the one-row-one-line invariant the
// torn-write repair depends on.
func marshalRow(r Record) []byte {
	cols := columns(r)
	var buf bytes.Buffer
	buf.WriteString("<tr")
	htmlledger.WriteAttr(&buf, "id", r.ID)
	for _, c := range cols {
		htmlledger.WriteAttr(&buf, "data-"+c.name, c.value)
	}
	buf.WriteString(">")
	for _, c := range cols {
		htmlledger.WriteCell(&buf, c.name, c.value)
	}
	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// ReadFile parses the ledger, ordered by start then id. A missing file reports
// os.IsNotExist. A row that fails validation is skipped, so one bad row never
// hides the others (as in the sibling ledgers).
func ReadFile(path string) ([]Record, error) {
	var records []Record
	err := ledgerFormat.ReadRows(path, func(row *goquery.Selection) {
		if r, ok := parseRow(row); ok {
			records = append(records, r)
		}
	})
	if err != nil {
		return nil, err
	}
	sortRecords(records)
	return records, nil
}

func parseRow(row *goquery.Selection) (Record, bool) {
	a := func(name string) string { return strings.TrimSpace(htmlledger.Attr(row, "data-"+name)) }
	r := Record{
		ID: a("run-id"), Mode: a("mode"), Harness: a("harness"), Name: a("name"),
		Label: htmlledger.Attr(row, "data-label"), WorkItem: a("work-item"), Link: a("link"),
		CallerSession: a("caller-session"), WorkerSessions: strings.Fields(a("worker-sessions")),
		Host: a("host"), EndReason: a("end-reason"), ResumedFrom: a("resumed-from"),
	}
	if len(r.WorkerSessions) == 0 {
		r.WorkerSessions = nil
	}
	var err error
	if r.Started, err = ParseTime(a("started")); err != nil {
		return Record{}, false
	}
	if r.Ended, err = ParseTime(a("ended")); err != nil {
		return Record{}, false
	}
	if v := a("exit"); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return Record{}, false
		}
		r.Exit = &n
	}
	r.UsageInput, _ = strconv.Atoi(a("usage-input"))
	r.UsageCached, _ = strconv.Atoi(a("usage-cached"))
	r.UsageOutput, _ = strconv.Atoi(a("usage-output"))
	r.UsageReasoning, _ = strconv.Atoi(a("usage-reasoning"))
	if r.Validate() != nil {
		return Record{}, false
	}
	return r, true
}

// sortRecords orders by start, then id, so the order is total and stable.
func sortRecords(recs []Record) {
	sort.SliceStable(recs, func(i, j int) bool {
		if !recs[i].Started.Equal(recs[j].Started) {
			return recs[i].Started.Before(recs[j].Started)
		}
		return recs[i].ID < recs[j].ID
	})
}

// appendRowLocked appends one row in constant time with torn-tail repair. The
// caller holds the write guard.
func appendRowLocked(path string, r Record) error {
	return ledgerFormat.AppendRow(path, marshalRow(r))
}

// writeAllLocked atomically rewrites path with records (the Close and Enrich
// path). The caller holds the write guard.
func writeAllLocked(path string, records []Record) error {
	sorted := append([]Record(nil), records...)
	sortRecords(sorted)
	rows := make([][]byte, 0, len(sorted))
	for _, r := range sorted {
		rows = append(rows, marshalRow(r))
	}
	return ledgerFormat.WriteAll(path, rows)
}
