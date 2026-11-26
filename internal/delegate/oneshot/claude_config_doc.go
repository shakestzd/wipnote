package oneshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Claude Code's config is not wipnote's file, so the seed copies it and edits
// only the members it sets. Why: an earlier seed decoded the whole file into Go
// values and wrote those back. That re-sorted every key, rewrote escapes,
// turned a lone surrogate escape into U+FFFD, replaced a "projects" value that
// was not an object, and dropped anything after the first JSON value
// (bug-65bf54aa).

// jsonMember is one member of a JSON object, with the key and the value as the
// bytes the file holds.
type jsonMember struct {
	rawKey []byte
	key    string
	val    json.RawMessage
}

// jsonObject holds an object's members in file order.
type jsonObject []jsonMember

var (
	errNotObject = errors.New("it is not a JSON object")
	jsonTrue     = json.RawMessage("true")
)

// parseJSONObject reads one JSON object and refuses anything after it.
func parseJSONObject(data []byte) (jsonObject, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errNotObject
	}
	obj := jsonObject{}
	for dec.More() {
		start := dec.InputOffset() // at this member's key, or at the comma before it
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := tok.(string)
		rawKey := bytes.TrimLeft(data[start:dec.InputOffset()], ", \t\r\n")
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		obj = append(obj, jsonMember{rawKey: rawKey, key: key, val: val})
	}
	if _, err := dec.Token(); err != nil { // the closing brace
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("data follows the JSON object")
	}
	return obj, nil
}

// get returns key's value. The last member wins, as it does in Claude Code.
func (o jsonObject) get(key string) (json.RawMessage, bool) {
	for i := len(o) - 1; i >= 0; i-- {
		if o[i].key == key {
			return o[i].val, true
		}
	}
	return nil, false
}

// set replaces key's value in place, or adds the member at the end.
func (o *jsonObject) set(key string, val json.RawMessage) {
	for i := len(*o) - 1; i >= 0; i-- {
		if (*o)[i].key == key {
			(*o)[i].val = val
			return
		}
	}
	*o = append(*o, jsonMember{rawKey: jsonString(key), key: key, val: val})
}

// child returns the object stored at key, or an empty one when key is absent.
// A value of any other type is an error: the seed must not replace it.
func (o jsonObject) child(key string) (jsonObject, error) {
	val, ok := o.get(key)
	if !ok {
		return jsonObject{}, nil
	}
	c, err := parseJSONObject(val)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", jsonString(key), err)
	}
	return c, nil
}

// raw is the object as compact JSON, every key and value as it was read.
func (o jsonObject) raw() json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(m.rawKey)
		b.WriteByte(':')
		b.Write(m.val)
	}
	b.WriteByte('}')
	return b.Bytes()
}

// jsonString is s as a JSON string, with <, > and & left as they are.
func jsonString(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s) // a string always encodes
	return bytes.TrimSpace(b.Bytes())
}

func isJSONTrue(v json.RawMessage) bool { return string(bytes.Trim(v, jsonSpace)) == "true" }

// jsonSpace is the white space JSON allows around a value. Go's TrimSpace also
// drops form feed and Unicode spaces, which are not JSON: trimming those let the
// seed rewrite a file that the reader refuses (bug-65bf54aa).
const jsonSpace = " \t\r\n"

// jsonLayout is how a config file is laid out: the indent unit and the line
// end. An empty line end means the file is on one line.
type jsonLayout struct{ indent, lineEnd string }

// claudeLayout is the layout Claude Code writes, used for a new or empty config.
var claudeLayout = jsonLayout{indent: "  ", lineEnd: "\n"}

// layoutOf reads the layout from the white space between an object's opening
// brace and its first key.
func layoutOf(body []byte) jsonLayout {
	rest := body[1:]
	ws := string(rest[:len(rest)-len(bytes.TrimLeft(rest, jsonSpace))])
	i := strings.LastIndexByte(ws, '\n')
	if i < 0 {
		return jsonLayout{}
	}
	l := jsonLayout{indent: ws[i+1:], lineEnd: "\n"}
	if strings.Contains(ws, "\r\n") {
		l.lineEnd = "\r\n"
	}
	return l
}

// render writes raw in the layout.
func (l jsonLayout) render(raw []byte) ([]byte, error) {
	if l.lineEnd == "" {
		var b bytes.Buffer
		err := json.Compact(&b, raw)
		return b.Bytes(), err
	}
	var b bytes.Buffer
	if err := json.Indent(&b, raw, "", l.indent); err != nil {
		return nil, err
	}
	if l.lineEnd == "\n" {
		return b.Bytes(), nil
	}
	// A JSON string holds no raw line feed, so every one is a line end.
	return bytes.ReplaceAll(b.Bytes(), []byte("\n"), []byte(l.lineEnd)), nil
}

// seedConfig returns data with each dir's trust flag set, and the onboarding
// flag when onboarding is true. changed false means every key was already set.
// The output keeps the file's own indent and line ends, so a file in a regular
// layout gains only the new lines. A file in an irregular layout is laid out
// again in its first indent, with every key and value kept. A file on one line
// that has a space after a colon or comma loses those spaces.
func seedConfig(data []byte, dirs []string, onboarding bool) (out []byte, changed bool, err error) {
	body := bytes.Trim(data, jsonSpace)
	head, tail, layout := []byte(nil), []byte("\n"), claudeLayout
	top := jsonObject{}
	if len(body) > 0 {
		if top, err = parseJSONObject(body); err != nil {
			return nil, false, err
		}
		head = data[:len(data)-len(bytes.TrimLeft(data, jsonSpace))]
		tail = data[len(bytes.TrimRight(data, jsonSpace)):]
		if len(top) > 0 {
			layout = layoutOf(body)
		}
	}
	if v, _ := top.get("hasCompletedOnboarding"); onboarding && !isJSONTrue(v) {
		top.set("hasCompletedOnboarding", jsonTrue)
		changed = true
	}
	projects, err := top.child("projects")
	if err != nil {
		return nil, false, err
	}
	for _, d := range dirs {
		p, err := projects.child(d)
		if err != nil {
			return nil, false, fmt.Errorf(`"projects": %w`, err)
		}
		if v, _ := p.get("hasTrustDialogAccepted"); !isJSONTrue(v) {
			p.set("hasTrustDialogAccepted", jsonTrue)
			projects.set(d, p.raw())
			changed = true
		}
	}
	if !changed {
		return nil, false, nil
	}
	top.set("projects", projects.raw())
	laid, err := layout.render(top.raw())
	if err != nil {
		return nil, false, err
	}
	// A new buffer: head and tail are slices of data, and an append to head
	// would write over the tail when data has spare capacity, as a file read does.
	out = make([]byte, 0, len(head)+len(laid)+len(tail))
	out = append(append(append(out, head...), laid...), tail...)
	return out, true, nil
}
