package htmlledger

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

var testFormat = Format{
	Name:        "testledger",
	Header:      "<!DOCTYPE html>\n<html><body><table data-test-ledger=\"true\"><tbody>\n",
	Footer:      "</tbody></table></body></html>\n",
	RowSelector: "table[data-test-ledger] tbody tr[data-id]",
	TempPattern: ".test-ledger-*.tmp",
}

func row(id, note string) []byte {
	var buf bytes.Buffer
	buf.WriteString("<tr")
	WriteAttr(&buf, "data-id", id)
	WriteAttr(&buf, "data-note", note)
	buf.WriteString(">")
	WriteCell(&buf, "note", note)
	buf.WriteString("</tr>\n")
	return buf.Bytes()
}

func readNotes(t *testing.T, path string) (ids, notes []string) {
	t.Helper()
	err := testFormat.ReadRows(path, func(r *goquery.Selection) {
		ids = append(ids, Attr(r, "data-id"))
		notes = append(notes, Attr(r, "data-note"))
	})
	if err != nil {
		t.Fatalf("ReadRows: %v", err)
	}
	return ids, notes
}

// TestRoundTrip_Escaping pins the escaping every ledger relied on: HTML
// specials survive a round trip, and newline/CR/tab become spaces so a row
// stays one line.
func TestRoundTrip_Escaping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "ledger.html")
	notes := []string{`<b>&"quoted"'s</b>`, "line1\nline2\r\tend", "", "plain ünïcode"}
	for i, n := range notes {
		if err := testFormat.AppendRow(path, row(string(rune('a'+i)), n)); err != nil {
			t.Fatal(err)
		}
	}
	_, got := readNotes(t, path)
	want := []string{`<b>&"quoted"'s</b>`, "line1 line2  end", "", "plain ünïcode"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("notes = %q, want %q", got, want)
	}
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != strings.Count(testFormat.Header+testFormat.Footer, "\n")+len(notes) {
		t.Errorf("file has %d lines; every row must be exactly one line:\n%s", n, data)
	}
	if !strings.HasPrefix(string(data), testFormat.Header) || !strings.HasSuffix(string(data), testFormat.Footer) {
		t.Errorf("file not bracketed by header/footer:\n%s", data)
	}
}

// TestAppendRow_RepairsTornTail pins the repair: a tail torn mid-row is
// dropped back to the last complete row; a file torn inside the header is
// rewritten from scratch.
func TestAppendRow_RepairsTornTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.html")
	for _, id := range []string{"a", "b"} {
		if err := testFormat.AppendRow(path, row(id, id)); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	torn := strings.TrimSuffix(string(data), testFormat.Footer) + `<tr data-id="c" data-no`
	if err := os.WriteFile(path, []byte(torn), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testFormat.AppendRow(path, row("d", "d")); err != nil {
		t.Fatal(err)
	}
	if ids, _ := readNotes(t, path); strings.Join(ids, ",") != "a,b,d" {
		t.Fatalf("ids after repair = %v, want a,b,d", ids)
	}

	if err := os.WriteFile(path, []byte(testFormat.Header[:10]), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testFormat.AppendRow(path, row("e", "e")); err != nil {
		t.Fatal(err)
	}
	if ids, _ := readNotes(t, path); strings.Join(ids, ",") != "e" {
		t.Fatalf("ids after header tear = %v, want e", ids)
	}
}

// TestAppendRow_NoLineBoundaryFails pins the failure side: a tail with no
// newline in the scan window is an error and the file is left as it was.
func TestAppendRow_NoLineBoundaryFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.html")
	junk := testFormat.Header[:len(testFormat.Header)-1] + strings.Repeat("x", tornScanWindow+10)
	if err := os.WriteFile(path, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := testFormat.AppendRow(path, row("a", "a")); err == nil {
		t.Fatal("AppendRow on a tail with no line boundary returned no error")
	}
	if data, _ := os.ReadFile(path); string(data) != junk {
		t.Error("failed append changed the file")
	}
}

// TestWriteAll_ReplacesAtomically pins the rewrite path: the file holds
// exactly the given rows, and no temp file is left behind.
func TestWriteAll_ReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ledger.html")
	if err := testFormat.AppendRow(path, row("old", "old")); err != nil {
		t.Fatal(err)
	}
	if err := testFormat.WriteAll(path, [][]byte{row("x", "1"), row("y", "2")}); err != nil {
		t.Fatal(err)
	}
	if ids, _ := readNotes(t, path); strings.Join(ids, ",") != "x,y" {
		t.Fatalf("ids = %v, want x,y", ids)
	}
	if err := testFormat.WriteAll(path, nil); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != testFormat.Header+testFormat.Footer {
		t.Errorf("empty WriteAll = %q", data)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("dir holds %d entries, want only the ledger", len(entries))
	}
}

// TestReadRows_MissingFile pins that a missing ledger reports os.IsNotExist.
func TestReadRows_MissingFile(t *testing.T) {
	err := testFormat.ReadRows(filepath.Join(t.TempDir(), "none.html"), func(*goquery.Selection) {})
	if !os.IsNotExist(err) {
		t.Fatalf("ReadRows(missing) = %v, want IsNotExist", err)
	}
}
