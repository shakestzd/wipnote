package gateledger

import (
	"bytes"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/shakestzd/wipnote/core/htmlledger"
)

// ledgerHeader and ledgerFooter bracket the row lines. They are byte-stable
// constants because the append path finds the write offset by matching the
// footer against the file's trailing bytes — any drift here must be a deliberate
// format change, and would be caught by the round-trip tests.
const ledgerHeader = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Gate Runs</title></head><body>
<main>
<table data-gate-ledger="true">
<thead><tr><th>Record</th><th>Work Item</th><th>Session</th><th>Harness</th><th>Project Type</th><th>Gate Command</th><th>Status</th><th>Checked</th><th>Source</th><th>Guards</th><th>Profile</th><th>Allowlist Hits</th><th>Summary</th></tr></thead>
<tbody>
`

const ledgerFooter = `</tbody>
</table>
</main>
</body></html>
`

// rowSelector is the parse contract: only <tr> elements inside the gate-ledger
// table that carry a record id are records. A torn tail that goquery renders as
// some other element is therefore ignored rather than mis-parsed.
const rowSelector = `table[data-gate-ledger] tbody tr[data-record-id]`

// ledgerFormat is this ledger's document shape for the shared HTML codec.
var ledgerFormat = htmlledger.Format{
	Name:        "gateledger",
	Header:      ledgerHeader,
	Footer:      ledgerFooter,
	RowSelector: rowSelector,
}

// marshalRow renders one gate run as exactly ONE line terminated by '\n'.
//
// The single-line invariant is what makes the torn-write repair sound: the
// repair truncates back to the last newline, which is always a row boundary. It
// is enforced by sanitizing every field, not merely assumed — and this ledger
// carries the fields most likely to test it. GateCommand is a joined shell
// command line, OutputSummary is gate stdout, and AllowlistHitsJSON embeds
// operator-written justification prose; any of the three can arrive with
// embedded newlines, and one such write would otherwise swallow every row after
// it.
func marshalRow(r Record) []byte {
	var buf bytes.Buffer
	buf.WriteString("<tr")
	htmlledger.WriteAttr(&buf, "id", r.ID)
	htmlledger.WriteAttr(&buf, "data-record-id", r.ID)
	htmlledger.WriteAttr(&buf, "data-work-item", r.WorkItemID)
	htmlledger.WriteAttr(&buf, "data-session", r.SessionID)
	htmlledger.WriteAttr(&buf, "data-harness", r.Harness)
	htmlledger.WriteAttr(&buf, "data-project-type", r.ProjectType)
	htmlledger.WriteAttr(&buf, "data-gate-command", r.GateCommand)
	htmlledger.WriteAttr(&buf, "data-status", r.Status)
	htmlledger.WriteAttr(&buf, "data-checked-at", FormatTime(r.CheckedAt))
	htmlledger.WriteAttr(&buf, "data-signature", r.Signature)
	htmlledger.WriteAttr(&buf, "data-source", r.Source)
	htmlledger.WriteAttr(&buf, "data-guards-run", r.GuardsRunJSON)
	htmlledger.WriteAttr(&buf, "data-profile-signature", r.ProfileSignature)
	htmlledger.WriteAttr(&buf, "data-allowlist-hits", r.AllowlistHitsJSON)
	htmlledger.WriteAttr(&buf, "data-allowlist-hit-count", strconv.Itoa(r.AllowlistHitCount))
	htmlledger.WriteAttr(&buf, "data-output-summary", r.OutputSummary)
	buf.WriteString(">")

	htmlledger.WriteCell(&buf, "record-id", r.ID)
	htmlledger.WriteCell(&buf, "work-item", r.WorkItemID)
	htmlledger.WriteCell(&buf, "session", r.SessionID)
	htmlledger.WriteCell(&buf, "harness", r.Harness)
	htmlledger.WriteCell(&buf, "project-type", r.ProjectType)
	htmlledger.WriteCell(&buf, "gate-command", r.GateCommand)
	htmlledger.WriteCell(&buf, "status", r.Status)
	htmlledger.WriteCell(&buf, "checked-at", FormatTime(r.CheckedAt))
	htmlledger.WriteCell(&buf, "source", r.Source)
	htmlledger.WriteCell(&buf, "guards-run", r.GuardsRunJSON)
	htmlledger.WriteCell(&buf, "profile-signature", r.ProfileSignature)
	htmlledger.WriteCell(&buf, "allowlist-hits", r.AllowlistHitsJSON)
	htmlledger.WriteCell(&buf, "output-summary", r.OutputSummary)

	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// ReadFile parses the ledger into records, ordered by checked-at. A missing file
// reports os.IsNotExist so callers can treat it as empty.
//
// Rows are read leniently: a row that fails validation is skipped rather than
// failing the whole file, so one bad row can never hide every other gate run.
// This matches the claim and sessions ledgers.
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
	r := Record{
		ID:                strings.TrimSpace(htmlledger.Attr(row, "data-record-id")),
		WorkItemID:        strings.TrimSpace(htmlledger.Attr(row, "data-work-item")),
		SessionID:         strings.TrimSpace(htmlledger.Attr(row, "data-session")),
		Harness:           strings.TrimSpace(htmlledger.Attr(row, "data-harness")),
		ProjectType:       strings.TrimSpace(htmlledger.Attr(row, "data-project-type")),
		GateCommand:       htmlledger.Attr(row, "data-gate-command"),
		Status:            strings.TrimSpace(htmlledger.Attr(row, "data-status")),
		Signature:         strings.TrimSpace(htmlledger.Attr(row, "data-signature")),
		Source:            strings.TrimSpace(htmlledger.Attr(row, "data-source")),
		GuardsRunJSON:     strings.TrimSpace(htmlledger.Attr(row, "data-guards-run")),
		ProfileSignature:  strings.TrimSpace(htmlledger.Attr(row, "data-profile-signature")),
		AllowlistHitsJSON: strings.TrimSpace(htmlledger.Attr(row, "data-allowlist-hits")),
		OutputSummary:     htmlledger.Attr(row, "data-output-summary"),
	}
	checked, err := ParseTime(htmlledger.Attr(row, "data-checked-at"))
	if err != nil {
		return Record{}, false
	}
	r.CheckedAt = checked
	if n, convErr := strconv.Atoi(strings.TrimSpace(htmlledger.Attr(row, "data-allowlist-hit-count"))); convErr == nil && n > 0 {
		r.AllowlistHitCount = n
	}
	if r.Validate() != nil {
		return Record{}, false
	}
	return r, true
}

// sortRecords orders by checked-at, then by id so the order is total and stable
// (two gate runs in different worktrees can share a timestamp). Callers that
// want "the latest" read from the tail.
func sortRecords(recs []Record) {
	sort.Slice(recs, func(i, j int) bool {
		if !recs[i].CheckedAt.Equal(recs[j].CheckedAt) {
			return recs[i].CheckedAt.Before(recs[j].CheckedAt)
		}
		return recs[i].ID < recs[j].ID
	})
}

// appendRowLocked appends one row at the tail of path in CONSTANT time: it
// overwrites the trailing footer with row+footer instead of parsing and
// reserializing every existing row.
//
// This is the ONLY writer in the package. A gate run is immutable once recorded,
// so unlike core/claimledger and core/sessionledger there is no update path and
// no atomic temp-then-rename rewriter to pair with it.
//
// The caller must already hold the write guard for path.
//
// TORN-WRITE GUARD (the reason this is not a naive seek-to-end append): a crash
// mid-append leaves a file whose tail is neither a complete row nor the footer.
// Appending after that tail would merge the new row INTO the corrupt fragment —
// goquery parses `<tr data-recor` + `<tr …>` as one garbage element and SWALLOWS
// the new row. So before writing we locate a known-good boundary: the footer if
// the file ends with it, otherwise the last newline, which is by construction the
// end of the last complete row.
func appendRowLocked(path string, r Record) error {
	return ledgerFormat.AppendRow(path, marshalRow(r))
}
