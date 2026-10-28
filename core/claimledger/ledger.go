package claimledger

import (
	"bytes"
	"github.com/shakestzd/wipnote/core/containerwrite"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/shakestzd/wipnote/core/fsprobe"
	"github.com/shakestzd/wipnote/core/htmlledger"
)

// ledgerHeader and ledgerFooter bracket the row lines. They are byte-stable
// constants because the append path finds the write offset by matching the
// footer against the file's trailing bytes — any drift here must be a
// deliberate format change, and would be caught by the round-trip tests.
const ledgerHeader = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Claim Episodes</title></head><body>
<main>
<table data-claim-ledger="true">
<thead><tr><th>Episode</th><th>Work Item</th><th>Session</th><th>Agent</th><th>Start</th><th>End</th><th>Outcome</th></tr></thead>
<tbody>
`

const ledgerFooter = `</tbody>
</table>
</main>
</body></html>
`

// rowSelector is the parse contract: only <tr> elements inside the claim-ledger
// table that carry an episode id are records. A torn tail that goquery renders
// as some other element is therefore ignored rather than mis-parsed.
const rowSelector = `table[data-claim-ledger] tbody tr[data-episode-id]`

// ledgerFormat is this ledger's document shape for the shared HTML codec.
var ledgerFormat = htmlledger.Format{
	Name:        "claimledger",
	Header:      ledgerHeader,
	Footer:      ledgerFooter,
	RowSelector: rowSelector,
	TempPattern: ".claims-*.tmp",
}

// marshalRow renders one episode as exactly ONE line terminated by '\n'.
//
// The single-line invariant is what makes the torn-write repair sound: the
// repair truncates back to the last newline, which is always a row boundary. It
// is enforced by sanitizing every field, not merely assumed — an ID or agent
// name carrying a newline would otherwise let one corrupt write swallow the
// rows after it.
func marshalRow(e Episode) []byte {
	var buf bytes.Buffer
	buf.WriteString("<tr")
	htmlledger.WriteAttr(&buf, "id", e.ID)
	htmlledger.WriteAttr(&buf, "data-episode-id", e.ID)
	htmlledger.WriteAttr(&buf, "data-work-item", e.WorkItemID)
	htmlledger.WriteAttr(&buf, "data-session", e.SessionID)
	htmlledger.WriteAttr(&buf, "data-root-session", e.RootSessionID)
	htmlledger.WriteAttr(&buf, "data-agent", e.AgentID)
	htmlledger.WriteAttr(&buf, "data-start", FormatTime(e.StartedAt))
	endStr := ""
	if !e.IsOpen() {
		endStr = FormatTime(e.EndedAt)
	}
	htmlledger.WriteAttr(&buf, "data-end", endStr)
	htmlledger.WriteAttr(&buf, "data-outcome", string(e.Outcome))
	buf.WriteString(">")

	htmlledger.WriteCell(&buf, "episode-id", e.ID)
	htmlledger.WriteCell(&buf, "work-item", e.WorkItemID)
	htmlledger.WriteCell(&buf, "session", e.SessionID)
	htmlledger.WriteCell(&buf, "agent", e.AgentID)
	htmlledger.WriteCell(&buf, "start", FormatTime(e.StartedAt))
	htmlledger.WriteCell(&buf, "end", endStr)
	htmlledger.WriteCell(&buf, "outcome", string(e.Outcome))

	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// ReadFile parses one claim-ledger file into episodes, ordered by start time.
// A missing file reports os.IsNotExist so callers can treat it as empty.
//
// Rows are read leniently: a row missing its required fields is skipped rather
// than failing the whole file, so one bad row can never hide a session's other
// episodes. This matches the archive ledger's behaviour.
func ReadFile(path string) ([]Episode, error) {
	fsprobe.Touch(fsprobe.OpRead, path)
	var episodes []Episode
	err := ledgerFormat.ReadRows(path, func(row *goquery.Selection) {
		if e, ok := parseRow(row); ok {
			episodes = append(episodes, e)
		}
	})
	if err != nil {
		return nil, err
	}
	sortEpisodes(episodes)
	return episodes, nil
}

func parseRow(row *goquery.Selection) (Episode, bool) {
	e := Episode{
		ID:            strings.TrimSpace(htmlledger.Attr(row, "data-episode-id")),
		WorkItemID:    strings.TrimSpace(htmlledger.Attr(row, "data-work-item")),
		SessionID:     strings.TrimSpace(htmlledger.Attr(row, "data-session")),
		RootSessionID: strings.TrimSpace(htmlledger.Attr(row, "data-root-session")),
		AgentID:       strings.TrimSpace(htmlledger.Attr(row, "data-agent")),
		Outcome:       Outcome(strings.TrimSpace(htmlledger.Attr(row, "data-outcome"))),
	}
	started, err := ParseTime(htmlledger.Attr(row, "data-start"))
	if err != nil {
		return Episode{}, false
	}
	e.StartedAt = started
	ended, err := ParseTime(htmlledger.Attr(row, "data-end"))
	if err != nil {
		return Episode{}, false
	}
	e.EndedAt = ended
	if e.Validate() != nil {
		return Episode{}, false
	}
	return e, true
}

// sortEpisodes orders by start, then by ID so the order is total and stable
// across rewrites (two episodes can share a start timestamp).
func sortEpisodes(eps []Episode) {
	sort.Slice(eps, func(i, j int) bool {
		if !eps[i].StartedAt.Equal(eps[j].StartedAt) {
			return eps[i].StartedAt.Before(eps[j].StartedAt)
		}
		return eps[i].ID < eps[j].ID
	})
}

// appendRowLocked appends one episode row at the tail of path in CONSTANT time:
// it overwrites the trailing footer with row+footer instead of parsing and
// reserializing every existing row the way the arch-card writer does.
//
// The caller must already hold the write guard for path.
//
// TORN-WRITE GUARD (the reason this is not a naive seek-to-end append): a crash
// mid-append leaves a file whose tail is neither a complete row nor the footer.
// Appending after that tail would merge the new row INTO the corrupt fragment —
// goquery would parse `<tr data-work-i` + `<tr …>` as one garbage element and
// SWALLOW the new row, exactly the failure internal/commitqueue's appendLineLocked
// guards against for NDJSON. So before writing we locate a known-good boundary:
// the footer if the file ends with it, otherwise the last newline, which is by
// construction the end of the last complete row.
func appendRowLocked(path string, e Episode) error {
	removeStaleTemps(filepath.Dir(path), time.Now())
	return ledgerFormat.AppendRow(path, marshalRow(e))
}

// openRowLocked adds e to the shard at path. A container worker's shard is
// its own file across the bind mount, where only a rename is atomic (Phase B
// B7), so there it is rewritten whole; elsewhere the row is appended.
func openRowLocked(path string, existing []Episode, e Episode) error {
	if containerwrite.Active() {
		return writeAllLocked(path, append(append([]Episode(nil), existing...), e))
	}
	return appendRowLocked(path, e)
}

// writeAllLocked rewrites path with exactly the given episodes, atomically via
// temp-then-rename. This is the CLOSE path: recording an end on an existing row
// is inherently a read-modify-write, so it pays the full serialize cost. Opens
// — the frequent operation — never come through here.
//
// The caller must already hold the write guard for path.
func writeAllLocked(path string, episodes []Episode) error {
	fsprobe.Touch(fsprobe.OpWrite, path)
	removeStaleTemps(filepath.Dir(path), time.Now())
	sorted := make([]Episode, len(episodes))
	copy(sorted, episodes)
	sortEpisodes(sorted)
	rows := make([][]byte, 0, len(sorted))
	for _, e := range sorted {
		rows = append(rows, marshalRow(e))
	}
	return ledgerFormat.WriteAll(path, rows)
}
