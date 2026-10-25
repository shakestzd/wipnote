// Package htmlledger is the shared codec for wipnote's append-only HTML
// ledgers (claims, gate runs, sessions): one <tr> per line between a fixed
// header and footer, appended in place with torn-write repair, rewritten
// atomically when a row changes, and read back with goquery.
//
// Each ledger package keeps its own row schema (marshalRow/parseRow) and
// passes its document shape as a Format.
package htmlledger

import (
	"bytes"
	"fmt"
	"html"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// tornScanWindow bounds how far back the repair scan looks for a clean line
// boundary. A torn write can only have lost part of ONE row plus the footer, so
// a window far larger than either is enough; a file with no newline inside the
// window is corrupt in a way this package will not guess at.
const tornScanWindow = 64 * 1024

// Format is one ledger's document shape.
//
// Header and Footer bracket the row lines. They are byte-stable because the
// append path finds the write offset by matching Footer against the file's
// trailing bytes. RowSelector picks the record rows, so a torn tail goquery
// renders as some other element is ignored. TempPattern is the CreateTemp
// pattern WriteAll uses in the ledger's directory. Name prefixes errors.
type Format struct {
	Name        string
	Header      string
	Footer      string
	RowSelector string
	TempPattern string
}

// Sanitize collapses newline, carriage return and tab to a space, which keeps
// the one-row-one-line invariant the torn-write repair depends on.
func Sanitize(v string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, v)
}

// WriteAttr appends ` name="value"` with value sanitized and HTML-escaped.
func WriteAttr(buf *bytes.Buffer, name, value string) {
	buf.WriteByte(' ')
	buf.WriteString(name)
	buf.WriteString(`="`)
	buf.WriteString(html.EscapeString(Sanitize(value)))
	buf.WriteByte('"')
}

// WriteCell appends `<td data-field="field">value</td>` with value sanitized
// and HTML-escaped.
func WriteCell(buf *bytes.Buffer, field, value string) {
	buf.WriteString(`<td data-field="`)
	buf.WriteString(field)
	buf.WriteString(`">`)
	buf.WriteString(html.EscapeString(Sanitize(value)))
	buf.WriteString("</td>")
}

// Attr returns the named attribute of sel, or "" when it is absent.
func Attr(sel *goquery.Selection, name string) string {
	if v, ok := sel.Attr(name); ok {
		return v
	}
	return ""
}

// ReadRows parses the ledger at path and calls each for every record row in
// document order. A missing file reports os.IsNotExist.
func (f Format) ReadRows(path string, each func(row *goquery.Selection)) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		return fmt.Errorf("%s: parse %s: %w", f.Name, path, err)
	}
	doc.Find(f.RowSelector).Each(func(_ int, row *goquery.Selection) { each(row) })
	return nil
}
