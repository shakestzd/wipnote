// Package render turns a delegated run's raw output into the run-folder
// files of the feat-a1df60c2 contract (plan-28b4a7f6 slice 7): clean.log,
// clean.md, last.md, the usage line and run.html. It is the Go port of the
// codex-herdr-pane mod's viewer filters (viewer/codex.jq, stream.jq, md.jq,
// clean.awk, joinfence.awk) and of its run page (hooks/runhtml.ts,
// hooks/livedoc.ts); testdata/gen.sh pins the outputs to the mod's.
//
// Everything is a pure function over bytes or a line-at-a-time filter with
// a little state, except View, which follows a live run folder.
package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

// splitLines splits data into lines the way jq -R and awk read them: on
// "\n", with a last line that has no newline still a line.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	s := string(data)
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}

// validText replaces each invalid UTF-8 byte with U+FFFD, as jq does.
func validText(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		b.WriteRune(r)
	}
	return b.String()
}

// collapse runs every whitespace run together into one space (jq's
// gsub("\\s+"; " "), whose \s is Unicode White_Space).
func collapse(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			space = true
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	if space {
		b.WriteByte(' ')
	}
	return b.String()
}

// cut keeps n characters and marks a cut with "...".
func cut(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "..."
}

// clip is collapse, then cut.
func clip(s string, n int) string { return cut(collapse(s), n) }

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// trimSpace trims Unicode whitespace (JavaScript's trim, jq's \s).
func trimSpace(s string) string { return strings.TrimFunc(s, unicode.IsSpace) }

// obj is one JSON object, its values left raw so jq's semantics (// falls
// through null and false; tojson keeps key order) can be kept.
type obj map[string]json.RawMessage

// parseObj parses a line that is a JSON object; anything else is not one.
func parseObj(line string) (obj, bool) {
	var o obj
	if err := json.Unmarshal([]byte(line), &o); err != nil || o == nil {
		return nil, false
	}
	return o, true
}

// sub is the object under key k, nil when it is not an object.
func (o obj) sub(k string) obj {
	if o == nil {
		return nil
	}
	var s obj
	if json.Unmarshal(o[k], &s) != nil {
		return nil
	}
	return s
}

// truthy is jq's truth: present and neither null nor false.
func truthy(r json.RawMessage) bool {
	t := string(bytes.TrimSpace(r))
	return t != "" && t != "null" && t != "false"
}

// alt is jq's a // b // ...: the first truthy value, else nil.
func alt(rs ...json.RawMessage) json.RawMessage {
	for _, r := range rs {
		if truthy(r) {
			return r
		}
	}
	return nil
}

// str is a JSON string's value.
func str(r json.RawMessage) (string, bool) {
	var s string
	if len(r) == 0 || r[0] != '"' || json.Unmarshal(r, &s) != nil {
		return "", false
	}
	return s, true
}

// tostring is jq's tostring: a string as it is, anything else as compact
// JSON (key order kept); a missing value is null.
func tostring(r json.RawMessage) string {
	if s, ok := str(r); ok {
		return s
	}
	if len(r) == 0 {
		return "null"
	}
	return tojson(r)
}

// textOr is the string at r, or def when r is not truthy.
func textOr(r json.RawMessage, def string) (string, bool) {
	if !truthy(r) {
		return def, true
	}
	return str(r)
}
