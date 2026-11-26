package oneshot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

func isJSONTrue(v json.RawMessage) bool { return string(bytes.TrimSpace(v)) == "true" }

// seedConfig returns data with each dir's trust flag set, and the onboarding
// flag when onboarding is true. changed false means every key was already set.
// The output is indented by two spaces, as Claude Code writes the file, so a
// file Claude Code wrote gains only the new lines.
func seedConfig(data []byte, dirs []string, onboarding bool) (out []byte, changed bool, err error) {
	body, tail := bytes.TrimSpace(data), []byte("\n")
	top := jsonObject{}
	if len(body) > 0 {
		if top, err = parseJSONObject(body); err != nil {
			return nil, false, err
		}
		tail = data[len(bytes.TrimRight(data, " \t\r\n")):]
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
	var b bytes.Buffer
	if err := json.Indent(&b, top.raw(), "", "  "); err != nil {
		return nil, false, err
	}
	b.Write(tail)
	return b.Bytes(), true, nil
}
