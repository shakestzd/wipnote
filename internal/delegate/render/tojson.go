package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// tojson re-serializes JSON as jq's tojson does: compact, key order kept,
// number literals as written, and strings escaped only where JSON requires
// (quote, backslash, control characters and DEL); other characters raw.
func tojson(r json.RawMessage) string {
	dec := json.NewDecoder(bytes.NewReader(r))
	dec.UseNumber()
	var b strings.Builder
	type level struct {
		obj bool
		n   int
	}
	var stack []level
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if d, ok := tok.(json.Delim); ok && (d == '}' || d == ']') {
			stack = stack[:len(stack)-1]
			b.WriteRune(rune(d))
			continue
		}
		if len(stack) > 0 {
			top := &stack[len(stack)-1]
			switch {
			case top.obj && top.n%2 == 1:
				b.WriteByte(':')
			case top.n > 0:
				b.WriteByte(',')
			}
			top.n++
		}
		switch v := tok.(type) {
		case json.Delim:
			stack = append(stack, level{obj: v == '{'})
			b.WriteRune(rune(v))
		case string:
			b.WriteString(jqQuote(v))
		case json.Number:
			b.WriteString(v.String())
		case bool:
			fmt.Fprint(&b, v)
		case nil:
			b.WriteString("null")
		}
	}
	return b.String()
}

// jqQuote quotes s as jq prints a string.
func jqQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, c := range s {
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteRune(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
