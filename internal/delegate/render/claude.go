package render

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// The stream-json filter (the mod's stream.jq) for `claude -p` and for ccx's
// run log: events parsed once into neutral items, formatted as text or as
// Markdown in which a tool call and its result, paired by tool_use_id, are
// shown together in one fold when the result arrives.

// clItem is one stream-json item: say, call, result or done.
type clItem struct {
	k, id, name, arg, text string
	err                    bool
	done                   clDone
}

type clDone struct {
	subtype, secs, turns, in, out string
	err                           bool
	cost                          json.RawMessage
}

// claudeEvents parses one line's items.
func claudeEvents(o obj) []clItem {
	typ, _ := str(o["type"])
	switch typ {
	case "assistant":
		var out []clItem
		for _, c := range contentOf(o) {
			switch t, _ := str(c["type"]); t {
			case "text":
				if s, ok := str(c["text"]); ok {
					out = append(out, clItem{k: "say", text: s})
				}
			case "tool_use":
				out = append(out, clItem{k: "call", id: idOf(c["id"]), name: tostring(c["name"]), arg: summary(c["input"])})
			}
		}
		return out
	case "user":
		var out []clItem
		for _, c := range contentOf(o) {
			if t, _ := str(c["type"]); t == "tool_result" {
				out = append(out, clItem{k: "result", id: idOf(c["tool_use_id"]), err: truthy(c["is_error"]), text: body(c["content"])})
			}
		}
		return out
	case "result":
		return []clItem{{k: "done", done: doneOf(o)}}
	}
	return nil
}

func contentOf(o obj) []obj {
	var items []obj
	_ = json.Unmarshal(o.sub("message")["content"], &items)
	return items
}

func idOf(r json.RawMessage) string {
	if !truthy(r) {
		return ""
	}
	return tostring(r)
}

// summary is a call's argument: its command, path, pattern, url,
// description or prompt, else its whole input as JSON, clipped to 100.
func summary(input json.RawMessage) string {
	in := obj{}
	_ = json.Unmarshal(input, &in)
	v := alt(in["command"], in["file_path"], in["path"], in["pattern"], in["url"], in["description"], in["prompt"])
	if v == nil {
		v = input
	}
	return clip(tostring(v), 100)
}

// body is a tool result's text: an array's text parts joined by a space.
func body(r json.RawMessage) string {
	var parts []json.RawMessage
	if json.Unmarshal(r, &parts) == nil && parts != nil {
		texts := make([]string, len(parts))
		for i, p := range parts {
			var o obj
			if json.Unmarshal(p, &o) == nil {
				if t := alt(o["text"]); t != nil {
					texts[i] = tostring(t)
				}
			}
		}
		return strings.Join(texts, " ")
	}
	if !truthy(r) {
		return ""
	}
	return tostring(r)
}

func doneOf(o obj) clDone {
	num := func(r json.RawMessage) string {
		if v := alt(r); v != nil {
			return tostring(v)
		}
		return "0"
	}
	var ms float64
	_ = json.Unmarshal(alt(o["duration_ms"]), &ms)
	sub, _ := textOr(o["subtype"], "result")
	u := o.sub("usage")
	return clDone{subtype: sub, err: truthy(o["is_error"]), secs: jqNumber(math.Floor(ms / 1000)),
		turns: num(o["num_turns"]), in: num(u["input_tokens"]), out: num(u["output_tokens"]), cost: o["total_cost_usd"]}
}

// doneText is the result line: subtype, time, turns, tokens and cost.
func (d clDone) text(subtype string) string {
	s := subtype
	if d.err {
		s += " (error)"
	}
	s += " in " + d.secs + "s, " + d.turns + " turns, " + d.in + " in / " + d.out + " out tokens"
	if truthy(d.cost) {
		s += ", $" + tostring(d.cost)
	}
	return s
}

func claudeTxt(it clItem) string {
	switch it.k {
	case "say":
		return it.text
	case "call":
		return "> " + it.name + ": " + it.arg
	case "result":
		s := "  = " + clip(it.text, 200)
		if it.err {
			s += " (error)"
		}
		return s
	}
	return "--- " + it.done.text(it.done.subtype) + " ---"
}

func block(it clItem) string { return result(it.text, 1500, it.err, "") }

func resulttail(it clItem) string {
	s := " · " + resultsize(it.text)
	if it.err {
		s += " · error"
	}
	return s
}

func claudeMD(it clItem) string {
	switch it.k {
	case "say":
		return "### Assistant\n\n" + prose(it.text) + "\n"
	case "call":
		return toolline(it.name, it.arg) + "\n"
	case "result":
		return fold("result"+resulttail(it), it.err, block(it)) + "\n"
	}
	return "*" + it.done.text(esc(it.done.subtype)) + "*\n"
}

// callfold is a call and its result as one fold.
func callfold(call, res clItem) string {
	return fold(call.name+": "+call.arg+resulttail(res), res.err, toolline(call.name, call.arg)+"\n\n"+block(res))
}

// jqNumber prints a computed number as jq does: integers without a
// fraction, others in their shortest form.
func jqNumber(f float64) string {
	a := math.Abs(f)
	switch {
	case f == math.Trunc(f) && a < 1e17:
		return strconv.FormatInt(int64(f), 10)
	case a >= 1e-5 && a < 1e17:
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}
