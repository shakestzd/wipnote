package runledger

import (
	"bytes"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"github.com/shakestzd/wipnote/core/htmlledger"
)

const ledgerHeader = `<!DOCTYPE html>
<html><head><meta charset="utf-8"><title>Background Runs</title></head><body>
<main>
<table data-run-ledger="true">
<thead><tr><th>Run</th><th>Phase</th><th>At</th><th>Task</th><th>Tool Use</th><th>Kind</th><th>Description</th><th>Command</th><th>Folder</th><th>Status</th><th>Exit</th><th>Log</th><th>Last Line</th><th>First Error</th><th>Commit</th><th>Session</th><th>Work Item</th><th>Harness</th></tr></thead>
<tbody>
`

const ledgerFooter = `</tbody>
</table>
</main>
</body></html>
`

var ledgerFormat = htmlledger.Format{
	Name:        "runledger",
	Header:      ledgerHeader,
	Footer:      ledgerFooter,
	RowSelector: `table[data-run-ledger] tbody tr[data-run-id]`,
	TempPattern: ".runs-ledger-*.tmp",
}

type column struct{ name, value string }

func columns(e Event) []column {
	exit := ""
	if e.Exit != nil {
		exit = strconv.Itoa(*e.Exit)
	}
	return []column{
		{"run-id", e.RunID}, {"phase", e.Phase}, {"at", FormatTime(e.At)},
		{"task-id", e.TaskID}, {"tool-use-id", e.ToolUseID}, {"kind", e.Kind},
		{"description", e.Description}, {"command", e.Command}, {"folder", e.Folder},
		{"status", e.Status}, {"exit", exit}, {"log-path", e.LogPath},
		{"last-line", e.LastLine}, {"first-error", e.FirstError}, {"commit", e.Commit},
		{"session", e.Session}, {"work-item", e.WorkItem}, {"harness", e.Harness},
	}
}

// marshalRow renders one event as exactly one line; htmlledger sanitizes every
// value, which keeps the one-row-one-line invariant torn-write repair needs.
func marshalRow(e Event) []byte {
	var buf bytes.Buffer
	buf.WriteString("<tr")
	for _, c := range columns(e) {
		htmlledger.WriteAttr(&buf, "data-"+c.name, c.value)
	}
	buf.WriteString(">")
	for _, c := range columns(e) {
		htmlledger.WriteCell(&buf, c.name, c.value)
	}
	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

// readFile parses one shard in file order. A row that fails validation is
// skipped, so one bad row never hides the others.
func readFile(path string) ([]Event, error) {
	var out []Event
	err := ledgerFormat.ReadRows(path, func(row *goquery.Selection) {
		if e, ok := parseRow(row); ok {
			out = append(out, e)
		}
	})
	return out, err
}

func parseRow(row *goquery.Selection) (Event, bool) {
	a := func(name string) string { return htmlledger.Attr(row, "data-"+name) }
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(a("at")))
	if err != nil {
		return Event{}, false
	}
	e := Event{
		RunID: a("run-id"), Phase: a("phase"), At: at, TaskID: a("task-id"),
		ToolUseID: a("tool-use-id"), Kind: a("kind"), Description: a("description"),
		Command: a("command"), Folder: a("folder"), Status: a("status"),
		LogPath: a("log-path"), LastLine: a("last-line"), FirstError: a("first-error"),
		Commit: a("commit"), Session: a("session"), WorkItem: a("work-item"), Harness: a("harness"),
	}
	if v := strings.TrimSpace(a("exit")); v != "" {
		n, convErr := strconv.Atoi(v)
		if convErr != nil {
			return Event{}, false
		}
		e.Exit = &n
	}
	if e.Validate() != nil {
		return Event{}, false
	}
	return e, true
}
